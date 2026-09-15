package plugin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"

	pluginsdk "github.com/hyuan280/Sonicore-PluginSDK/go"
	"github.com/hyuan280/Sonicore-PluginSDK/go/gen"

	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/core/port"
	"github.com/sonicore/server/internal/infrastructure/logger"
	"github.com/sonicore/server/internal/infrastructure/task"
)

// Lifecycle timings (host side).
const (
	// handshakeTimeout bounds how long the plugin may take to print its
	// protocol line on stdout; exceeding it marks the plugin errored.
	handshakeTimeout = 15 * time.Second
	// shutdownTimeout bounds the graceful Shutdown RPC and the exit wait
	// before the manager force-kills the process.
	shutdownTimeout = 5 * time.Second
	// heartbeatInterval is the health-check period for running plugins.
	heartbeatInterval = 15 * time.Second
)

// Manager discovers plugin directories, runs their processes and adapts
// them into the registered port interfaces.
type Manager struct {
	dir        string
	repo       *Repo
	register   func(port.Notifier)
	unregister func(domain.ChannelType)
	controller func(domain.ChannelType, func(context.Context, bool) error)
	sched      *task.Scheduler

	mu        sync.Mutex
	running   map[string]*runningPlugin
	starting  map[string]bool
	manifests map[string]*pluginsdk.Manifest
	stopCh    chan struct{}
	stopOnce  sync.Once
	// locks holds one mutex per plugin id, serializing enable/disable and
	// update for the same plugin so an update's directory swap can never race
	// a restart of the old binary.
	locks sync.Map
}

type runningPlugin struct {
	client    *goplugin.Client
	cmd       *exec.Cmd
	lifecycle gen.LifecycleServiceClient
	tasks     gen.TaskServiceClient
	ui        gen.UIServiceClient
	adapter   *notifierAdapter
	enabled   bool
	// logSink closes the plugin's log file writer
	// ({data_dir}/log/plugins/{name}.log) when the process stops.
	logSink io.Closer
}

// NewManager creates a plugin manager. register receives every notifier
// plugin adapter (typically notifService.RegisterNotifier); controller
// installs the channel enable/disable control (notifService
// .RegisterChannelController); unregister removes a channel again
// (notifService.UnregisterNotifier); sched is the central task system that
// plugin-declared schedules are registered into.
func NewManager(pluginsDir string, repo *Repo, register func(port.Notifier), controller func(domain.ChannelType, func(context.Context, bool) error), unregister func(domain.ChannelType), sched *task.Scheduler) *Manager {
	return &Manager{
		dir:        pluginsDir,
		repo:       repo,
		register:   register,
		unregister: unregister,
		controller: controller,
		sched:      sched,
		running:    make(map[string]*runningPlugin),
		starting:   make(map[string]bool),
		manifests:  make(map[string]*pluginsdk.Manifest),
		stopCh:     make(chan struct{}),
	}
}

// Start creates the plugins directory, discovers plugins and starts every
// installed-and-enabled one. Newly discovered plugins (no DB row yet) are
// recorded as NOT installed and must be installed by the admin first.
// Errors for a single plugin never abort the server: the plugin is marked
// with an error status instead.
func (m *Manager) Start(ctx context.Context) error {
	go m.heartbeatLoop()

	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return fmt.Errorf("create plugins dir: %w", err)
	}
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return fmt.Errorf("read plugins dir: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(m.dir, entry.Name())
		manifest, err := pluginsdk.LoadManifest(filepath.Join(dir, "manifest.toml"))
		if err != nil {
			logger.Warn("[plugin] skip %s: %v", entry.Name(), err)
			continue
		}
		if err := manifest.Validate(); err != nil {
			logger.Warn("[plugin] skip %s: %v", entry.Name(), err)
			continue
		}
		inst := &Instance{
			ID:          manifest.Plugin.Name,
			Name:        manifest.Plugin.Name,
			Version:     manifest.Plugin.Version,
			Description: manifest.Plugin.Description,
			Source:      "local",
			Dir:         entry.Name(),
		}
		if err := m.repo.Upsert(ctx, inst); err != nil {
			logger.Error("[plugin] persist %s: %v", entry.Name(), err)
			continue
		}
		m.mu.Lock()
		m.manifests[inst.ID] = manifest
		m.mu.Unlock()

		enabled, installed, dirName, err := m.repo.GetRuntimeState(ctx, inst.ID)
		if err != nil {
			logger.Error("[plugin] load runtime state for %s: %v", inst.ID, err)
			continue
		}
		if !installed {
			continue
		}
		if !enabled {
			if err := m.repo.SetHasPage(ctx, inst.ID, false); err != nil {
				logger.Warn("[plugin] clear has_page for %s: %v", inst.ID, err)
			}
			continue
		}
		if dirName == "" {
			dirName = entry.Name()
		}
		if err := m.start(ctx, filepath.Join(m.dir, dirName), manifest); err != nil {
			logger.Error("[plugin] start %s failed: %v", inst.Name, err)
			logDBError(ctx, m.repo, inst.ID, StatusError, err.Error())
		} else if err := m.refreshHasPage(ctx, inst.ID); err != nil {
			logger.Warn("[plugin] probe data page for %s: %v", inst.ID, err)
		}
	}
	return nil
}

// start launches one plugin process, negotiates the handshake and registers
// the extension-point adapters the plugin declares (currently notifier).
// The plugin becomes visible via repo.List.
//
// The plugin's stdout/stderr are NOT wired through (SyncStdout/SyncStderr
// left at their io.Discard defaults): only the handshake protocol line is
// read from stdout, and plugin logging goes through sdk.Log →
// HostService.Log instead. Everything else the process prints is
// deliberately dropped (see hclogAdapter.StandardWriter): plugins
// self-report their diagnostics via the SDK, whose Run recovers panics and
// reports them, so unstructured stdout/stderr forwarding would only
// duplicate output into the dedicated per-plugin log file.
func (m *Manager) start(ctx context.Context, dir string, manifest *pluginsdk.Manifest) error {
	name := manifest.Plugin.Name
	// Atomic check-and-set: the starting placeholder prevents two concurrent
	// enable requests from launching the same plugin twice (startup up to
	// the handshake takes up to handshakeTimeout, so a plain
	// check-then-act over m.running would race).
	m.mu.Lock()
	if _, ok := m.running[name]; ok {
		m.mu.Unlock()
		return fmt.Errorf("already running")
	}
	if m.starting[name] {
		m.mu.Unlock()
		return fmt.Errorf("already starting")
	}
	m.starting[name] = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.starting, name)
		m.mu.Unlock()
	}()

	// Deployment convention: the plugin binary is named after the plugin
	// (manifest name), so go-plugin's logStderr derivation
	// (Named(filepath.Base(binary path))) naturally carries the plugin's
	// own name — the hclog adapter dedupes that append (see Named).
	bin := filepath.Join(dir, name)
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("plugin executable not found: %v", err)
	}

	// The plugin's log stream goes to its own file
	// ({data_dir}/log/plugins/{name}.log) — not into sonicore.log — plus
	// the stderr console mirror. Failure to open the sink is not fatal: the
	// plugin still runs, only its dedicated log file is missing.
	plog, logSink, err := logger.OpenPluginLog(name)
	if err != nil {
		logger.Warn("[plugin] open log sink for %s: %v", name, err)
		plog, logSink = nil, nil
	}
	// Every failure path below (launch/connect/dispense/init) must close
	// the log sink; once the running entry is registered the sink belongs
	// to it and success flips the flag.
	success := false
	defer func() {
		if !success && logSink != nil {
			_ = logSink.Close()
		}
	}()

	cmd := exec.Command(bin)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		pluginsdk.PluginIDEnv+"="+name,
		pluginsdk.PluginDirEnv+"="+dir,
	)

	var host *notifierHostPlugin
	hs := newHostService(name, m.repo, func(spec *gen.ScheduleSpec) error {
		if host == nil || host.tasks == nil {
			return fmt.Errorf("%w: task service not ready", ErrSchedulerUnavailable)
		}
		return m.registerSchedule(name, spec, host.tasks)
	}, plog)
	host = &notifierHostPlugin{hs: hs}
	client := goplugin.NewClient(&goplugin.ClientConfig{
		HandshakeConfig: pluginsdk.Handshake,
		Plugins: map[string]goplugin.Plugin{
			name: host,
		},
		Cmd:              cmd,
		AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolGRPC},
		Managed:          true,
		// Handshake timeout: a plugin that never prints its protocol line
		// fails Start, and the caller marks it errored.
		StartTimeout: handshakeTimeout,
		// Route go-plugin's hclog output through the plugin's own log sink
		// so internal lifecycle lines land in the same per-plugin file.
		Logger: newHCLogAdapter(name, plog),
	})
	if _, err := client.Start(); err != nil {
		client.Kill()
		return fmt.Errorf("launch process: %w", err)
	}

	proto, err := client.Client()
	if err != nil {
		client.Kill()
		return fmt.Errorf("connect: %w", err)
	}
	raw, err := proto.Dispense(name)
	if err != nil {
		client.Kill()
		return fmt.Errorf("dispense %s: %w", name, err)
	}
	hostPlugin, ok := raw.(*notifierHostPlugin)
	if !ok {
		client.Kill()
		return fmt.Errorf("dispense %s: unexpected client type %T", name, raw)
	}

	initCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	// Push the current configuration with Init so the plugin's SDK can cache
	// it (CurrentConfig) without an extra RPC. When the admin has never
	// configured the plugin, synthesize the manifest-declared defaults so the
	// plugin starts with sensible values instead of "no configuration".
	// A storage error here (not "no row") must NOT trigger the default
	// write — that would overwrite an existing user config during a
	// transient DB failure.
	configJSON, err := m.repo.GetConfig(initCtx, name)
	configLoadFailed := err != nil
	if configLoadFailed {
		logger.Warn("[plugin] load config for %s at init (defaults skipped): %v", name, err)
		configJSON = ""
	}
	if configJSON == "" && !configLoadFailed {
		if defaults := defaultConfigJSON(manifest); defaults != "" {
			configJSON = defaults
			if err := m.repo.SetConfig(initCtx, name, defaults); err != nil {
				logger.Warn("[plugin] persist default config for %s: %v", name, err)
			}
		}
	}
	if _, err := hostPlugin.lifecycle.Init(initCtx, &gen.InitRequest{
		ApiVersion:   pluginsdk.ABIVersion,
		HostBrokerId: hostPlugin.brokerID,
		ConfigJson:   configJSON,
	}); err != nil {
		client.Kill()
		return fmt.Errorf("init: %w", err)
	}

	var adapter *notifierAdapter
	if manifest.Plugin.ImplementsService("notifier") {
		adapter = &notifierAdapter{
			name:   name,
			client: hostPlugin.client,
			running: func() bool {
				m.mu.Lock()
				defer m.mu.Unlock()
				rp, ok := m.running[name]
				return ok && rp.enabled
			},
		}
		m.register(adapter)
		// The channel is enabled/disabled through the generic notification
		// channel control — the service never learns it is a plugin.
		if m.controller != nil {
			m.controller(domain.ChannelType(name), func(ctx context.Context, enabled bool) error {
				return m.SetEnabled(ctx, name, enabled)
			})
		}
	}

	rp := &runningPlugin{
		client:    client,
		cmd:       cmd,
		lifecycle: hostPlugin.lifecycle,
		tasks:     hostPlugin.tasks,
		ui:        hostPlugin.ui,
		adapter:   adapter,
		enabled:   true,
		logSink:   logSink,
	}
	// Register under the lock while re-checking the shutdown signal: Stop
	// closes stopCh BEFORE snapshotting m.running, so a start that is still
	// inside its handshake window either gets snapshot+stopped by Stop or
	// aborts here — never both-miss.
	m.mu.Lock()
	select {
	case <-m.stopCh:
		m.mu.Unlock()
		client.Kill()
		return fmt.Errorf("manager stopped during start")
	default:
	}
	m.running[name] = rp
	m.mu.Unlock()
	success = true

	// A disable/uninstall may have raced the launch window: the persisted
	// state is authoritative, so stop right away when it no longer wants
	// this plugin running.
	enabledNow, installedNow, _, stateErr := m.repo.GetRuntimeState(ctx, name)
	if stateErr == nil && (!installedNow || !enabledNow) {
		m.mu.Lock()
		delete(m.running, name)
		m.mu.Unlock()
		m.stopPlugin(name, rp)
		if err := m.repo.SetHasPage(ctx, name, false); err != nil {
			logger.Warn("[plugin] clear has_page for %s: %v", name, err)
		}
		if installedNow && !enabledNow {
			logDBError(ctx, m.repo, name, StatusDisabled, "")
		}
		logger.Info("[plugin] stopped %s (state changed while starting)", name)
		return nil
	}

	if err := m.repo.UpdateStatus(ctx, name, StatusOK, ""); err != nil {
		logger.Warn("[plugin] persist ok status for %s: %v", name, err)
	}
	// Host lifecycle lines stay in the host log only — the plugin's own
	// log file carries plugin-side output exclusively.
	logger.Info("[plugin] started %s v%s", name, manifest.Plugin.Version)

	go m.watchExit(name, rp)
	return nil
}

// watchExit marks the plugin as failed when its process dies without being
// stopped by the manager (go-plugin v1 exposes Exited() as a polled bool).
// The exit reason (signal/exit code) is included in the status message.
func (m *Manager) watchExit(name string, rp *runningPlugin) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if !rp.client.Exited() {
			continue
		}
		m.mu.Lock()
		_, ok := m.running[name]
		if ok {
			delete(m.running, name)
		}
		m.mu.Unlock()
		if !ok {
			return
		}
		reason := exitReason(rp.cmd)
		msg := "plugin process exited" + reason
		logDBError(context.Background(), m.repo, name, StatusError, msg)
		if err := m.repo.SetHasPage(context.Background(), name, false); err != nil {
			logger.Warn("[plugin] clear has_page for %s: %v", name, err)
		}
		// Drop the dead plugin's schedules too — the scheduler would
		// otherwise keep retrying RunTask against a dead process.
		m.removePluginTasks(name)
		if rp.logSink != nil {
			_ = rp.logSink.Close()
		}
		logger.Warn("[plugin] %s %s", name, msg)
		return
	}
}

// defaultConfigJSON builds a config JSON document from the manifest's
// declared field defaults ("" when no field declares a default).
func defaultConfigJSON(manifest *pluginsdk.Manifest) string {
	values := make(map[string]any, len(manifest.Plugin.Config))
	for key, field := range manifest.Plugin.Config {
		if field.Default != nil {
			values[key] = field.Default
		}
	}
	if len(values) == 0 {
		return ""
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return ""
	}
	return string(raw)
}

// exitReason describes how the plugin process ended (signal name or exit
// code), or "" when it cannot be determined.
func exitReason(cmd *exec.Cmd) string {
	if cmd == nil || cmd.ProcessState == nil {
		return ""
	}
	if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return fmt.Sprintf(" (signal: %s)", ws.Signal())
	}
	return fmt.Sprintf(" (exit code %d)", cmd.ProcessState.ExitCode())
}

// registerSchedule converts a plugin-declared schedule into the central
// task system: the central scheduler owns timing, status and manual
// controls, and the callback routes each run back into the plugin process.
// Task IDs are namespaced as "plugin:<name>:<taskID>", with Source=plugin
// and Provider=<plugin name> so the admin task UI can group them.
// ErrSchedulerUnavailable marks host-side failures of the task scheduler
// (not initialized yet / unavailable) so the host service can map them to
// codes.Unavailable instead of blaming the plugin's schedule spec.
var ErrSchedulerUnavailable = errors.New("task scheduler unavailable")

// registerSchedule installs a plugin-declared schedule into the central
// task scheduler.
func (m *Manager) registerSchedule(pluginID string, spec *gen.ScheduleSpec, tasks gen.TaskServiceClient) error {
	if m.sched == nil {
		return fmt.Errorf("%w", ErrSchedulerUnavailable)
	}
	var interval time.Duration
	if spec.Cron == "" {
		var err error
		interval, err = time.ParseDuration(spec.Interval)
		if err != nil {
			return fmt.Errorf("invalid interval %q: %w", spec.Interval, err)
		}
		if interval < time.Second {
			return fmt.Errorf("schedule interval must be at least 1s")
		}
	}
	return m.sched.Register(task.Spec{
		ID:       pluginTaskID(pluginID, spec.Id),
		Source:   "plugin",
		Provider: pluginID,
		Name:     spec.Name,
		Interval: interval,
		Cron:     spec.Cron,
	}, func(ctx context.Context) error {
		runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		resp, err := tasks.RunTask(runCtx, &gen.RunTaskRequest{TaskId: spec.Id})
		if err != nil {
			return err
		}
		if resp.Error != "" {
			return errors.New(resp.Error)
		}
		return nil
	})
}

func pluginTaskID(pluginID, taskID string) string {
	return "plugin:" + pluginID + ":" + taskID
}

// removePluginTasks drops every central task registered by a plugin
// (called when the plugin stops or is uninstalled). Re-enabling the plugin
// re-registers its schedules; the central store preserves the persisted
// enabled state keyed by task ID.
func (m *Manager) removePluginTasks(pluginID string) {
	if m.sched == nil {
		return
	}
	for _, t := range m.sched.List() {
		if t.Source == "plugin" && t.Provider == pluginID {
			m.sched.Remove(t.ID)
		}
	}
}

// stopPlugin asks the plugin to stop gracefully (Shutdown RPC), waits for
// the process to exit, falls back to killing it on timeout and drops its
// schedules. Safe to call after the entry was already removed from
// m.running.
func (m *Manager) stopPlugin(name string, rp *runningPlugin) {
	if rp.lifecycle != nil {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		if _, err := rp.lifecycle.Shutdown(ctx, &gen.ShutdownRequest{}); err != nil {
			logger.Debug("[plugin] %s shutdown rpc: %v", name, err)
		}
		cancel()

		deadline := time.Now().Add(shutdownTimeout)
		for time.Now().Before(deadline) {
			if rp.client.Exited() {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	// Kill fast-paths when the process already exited (its internal 2s
	// graceful window) and force-kills otherwise.
	rp.client.Kill()
	m.removePluginTasks(name)
	if rp.logSink != nil {
		_ = rp.logSink.Close()
		rp.logSink = nil
	}
	logger.Info("[plugin] stopped %s", name)
}

// Stop shuts every plugin process down. The lock is only held while
// snapshotting: stopPlugin blocks up to shutdownTimeout per plugin, so
// holding m.mu across all of them would stall heartbeat/SetEnabled/List
// for N × ~10s.
func (m *Manager) Stop() {
	m.stopOnce.Do(func() { close(m.stopCh) })
	m.mu.Lock()
	running := m.running
	m.running = make(map[string]*runningPlugin)
	m.mu.Unlock()
	for name, rp := range running {
		m.stopPlugin(name, rp)
	}
}

// pluginLock returns the per-plugin mutex serializing enable/disable and
// update for one plugin.
func (m *Manager) pluginLock(id string) *sync.Mutex {
	v, _ := m.locks.LoadOrStore(id, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// SetEnabled enables or disables one plugin. Disabling stops its process;
// enabling starts it again. The whole operation runs under the plugin's lock,
// so an enable can never race a concurrent update's directory swap.
func (m *Manager) SetEnabled(ctx context.Context, id string, enabled bool) error {
	lk := m.pluginLock(id)
	lk.Lock()
	defer lk.Unlock()

	if err := m.repo.SetEnabled(ctx, id, enabled); err != nil {
		return err
	}

	if !enabled {
		m.mu.Lock()
		rp, running := m.running[id]
		if running {
			delete(m.running, id)
		}
		m.mu.Unlock()
		if running {
			m.stopPlugin(id, rp)
			logDBError(ctx, m.repo, id, StatusDisabled, "")
		}
		// A stopped plugin cannot serve its data page anymore.
		if err := m.repo.SetHasPage(ctx, id, false); err != nil {
			logger.Warn("[plugin] clear has_page for %s: %v", id, err)
		}
		return nil
	}

	m.mu.Lock()
	_, running := m.running[id]
	m.mu.Unlock()
	if running {
		return nil
	}
	_, installed, dirName, err := m.repo.GetRuntimeState(ctx, id)
	if err != nil {
		return fmt.Errorf("load runtime state: %w", err)
	}
	if !installed {
		return fmt.Errorf("plugin %s is not installed", id)
	}
	if dirName == "" {
		dirName = id
	}
	manifest, err := pluginsdk.LoadManifest(filepath.Join(m.dir, dirName, "manifest.toml"))
	if err != nil {
		// Roll the flag back and record the failure so the admin UI does
		// not show an enabled plugin that is not actually running.
		_ = m.repo.SetEnabled(ctx, id, false)
		logDBError(ctx, m.repo, id, StatusError, "enable failed: "+err.Error())
		return fmt.Errorf("load manifest: %w", err)
	}
	m.mu.Lock()
	m.manifests[id] = manifest
	m.mu.Unlock()
	if err := m.start(ctx, filepath.Join(m.dir, dirName), manifest); err != nil {
		_ = m.repo.SetEnabled(ctx, id, false)
		logDBError(ctx, m.repo, id, StatusError, "enable failed: "+err.Error())
		return err
	}
	if err := m.refreshHasPage(ctx, id); err != nil {
		logger.Warn("[plugin] probe data page for %s: %v", id, err)
	}
	return nil
}

// refreshHasPage probes the running plugin's get_page once and persists the
// result (has_page). The frontend list response carries it, so the admin UI
// never needs to call get_page per plugin.
func (m *Manager) refreshHasPage(ctx context.Context, id string) error {
	schema, err := m.GetPage(ctx, id)
	if err != nil {
		return err
	}
	return m.repo.SetHasPage(ctx, id, schema != "")
}

// heartbeatLoop health-checks every running plugin periodically. A failed
// ping means the process is hung: it is killed and marked errored.
func (m *Manager) heartbeatLoop() {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.heartbeat()
		}
	}
}

func (m *Manager) heartbeat() {
	m.mu.Lock()
	snapshot := make(map[string]*runningPlugin, len(m.running))
	for k, v := range m.running {
		snapshot[k] = v
	}
	m.mu.Unlock()

	for name, rp := range snapshot {
		if ping(rp.client) == nil {
			continue
		}
		m.mu.Lock()
		_, ok := m.running[name]
		if ok {
			delete(m.running, name)
		}
		m.mu.Unlock()
		if !ok {
			continue
		}
		logger.Warn("[plugin] %s health check failed", name)
		rp.client.Kill()
		logDBError(context.Background(), m.repo, name, StatusError, "health check failed")
		if err := m.repo.SetHasPage(context.Background(), name, false); err != nil {
			logger.Warn("[plugin] clear has_page for %s: %v", name, err)
		}
		m.removePluginTasks(name)
		if rp.logSink != nil {
			_ = rp.logSink.Close()
		}
	}
}

func ping(client *goplugin.Client) error {
	proto, err := client.Client()
	if err != nil {
		return err
	}
	return proto.Ping()
}

// ConfigSchema returns the plugin's declared config fields (sorted by key)
// and whether the plugin is known. The host admin UI renders these fields as
// a form; the stored values live in plugin_config.
func (m *Manager) ConfigSchema(id string) ([]pluginsdk.ConfigField, bool) {
	m.mu.Lock()
	manifest, ok := m.manifests[id]
	m.mu.Unlock()
	if !ok {
		return nil, false
	}
	fields := make([]pluginsdk.ConfigField, 0, len(manifest.Plugin.Config))
	for _, f := range manifest.Plugin.Config {
		fields = append(fields, f)
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Key < fields[j].Key })
	return fields, true
}

// GetConfig returns the plugin's current configuration values (empty map
// when nothing is stored yet).
func (m *Manager) GetConfig(ctx context.Context, id string) (map[string]any, error) {
	raw, err := m.repo.GetConfig(ctx, id)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			return nil, fmt.Errorf("parse plugin config: %w", err)
		}
	}
	return out, nil
}

// uiClient returns the running plugin's UIService client (nil when the
// plugin is not running).
func (m *Manager) uiClient(id string) gen.UIServiceClient {
	m.mu.Lock()
	rp, ok := m.running[id]
	m.mu.Unlock()
	if !ok {
		return nil
	}
	return rp.ui
}

// GetForm fetches the plugin's config form schema JSON ("" when the plugin
// provides no form). The frontend falls back to the manifest schema.
func (m *Manager) GetForm(ctx context.Context, id string) (string, error) {
	ui := m.uiClient(id)
	if ui == nil {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	resp, err := ui.GetForm(ctx, &gen.GetFormRequest{})
	if err != nil {
		return "", fmt.Errorf("plugin %s get form: %w", id, err)
	}
	return resp.SchemaJson, nil
}

// GetPage fetches the plugin's data page schema JSON ("" when the plugin
// provides no page: the frontend then shows only the config form).
func (m *Manager) GetPage(ctx context.Context, id string) (string, error) {
	ui := m.uiClient(id)
	if ui == nil {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	resp, err := ui.GetPage(ctx, &gen.GetPageRequest{})
	if err != nil {
		return "", fmt.Errorf("plugin %s get page: %w", id, err)
	}
	return resp.SchemaJson, nil
}

// Uninstall soft-uninstalls a plugin: the process stops, its channels and
// tasks are removed and the row flips to installed=false. The plugin files
// and the stored config/data are kept so a reinstall restores them.
func (m *Manager) Uninstall(ctx context.Context, id string) error {
	// Existence check first: a missing row surfaces as sql.ErrNoRows so the
	// REST layer can answer 404 instead of a silent no-op.
	if _, _, _, err := m.repo.GetRuntimeState(ctx, id); err != nil {
		return err
	}
	m.mu.Lock()
	rp, ok := m.running[id]
	if ok {
		delete(m.running, id)
	}
	manifest, hasManifest := m.manifests[id]
	m.mu.Unlock()
	if ok {
		m.stopPlugin(id, rp)
	}
	if m.unregister != nil && hasManifest && manifest.Plugin.ImplementsService("notifier") {
		m.unregister(domain.ChannelType(id))
	}
	if err := m.repo.SetHasPage(ctx, id, false); err != nil {
		logger.Warn("[plugin] clear has_page for %s: %v", id, err)
	}
	if err := m.repo.UpdateStatus(ctx, id, StatusStopped, ""); err != nil {
		logger.Warn("[plugin] mark %s stopped: %v", id, err)
	}
	return m.repo.SetInstalled(ctx, id, false)
}

// Delete permanently removes a plugin: stops its process, unregisters its
// channels, deletes the plugin directory on disk and drops the DB row
// (cascade deletes the config/data).
func (m *Manager) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	rp, ok := m.running[id]
	if ok {
		delete(m.running, id)
	}
	manifest, hasManifest := m.manifests[id]
	delete(m.manifests, id)
	m.mu.Unlock()
	if ok {
		m.stopPlugin(id, rp)
	}
	if m.unregister != nil && hasManifest && manifest.Plugin.ImplementsService("notifier") {
		m.unregister(domain.ChannelType(id))
	}
	_, _, dirName, err := m.repo.GetRuntimeState(ctx, id)
	if err != nil {
		return fmt.Errorf("load runtime state: %w", err)
	}
	// RemoveAll is destructive: refuse anything that could escape the
	// plugins dir ("..", separators, absolute paths).
	if dirName == "" || dirName == "." || dirName == ".." || filepath.Base(dirName) != dirName {
		return fmt.Errorf("refusing to delete plugin %s: invalid dir %q", id, dirName)
	}
	dirPath := filepath.Join(m.dir, dirName)
	if clean := filepath.Clean(dirPath); !strings.HasPrefix(clean, filepath.Clean(m.dir)+string(filepath.Separator)) {
		return fmt.Errorf("refusing to delete plugin %s: dir %q escapes plugins dir", id, dirName)
	}
	if err := os.RemoveAll(dirPath); err != nil {
		return fmt.Errorf("remove plugin dir: %w", err)
	}
	return m.repo.Delete(ctx, id)
}

// Install flips a discovered plugin to installed. Installations default to
// disabled — the admin enables the plugin explicitly afterwards.
func (m *Manager) Install(ctx context.Context, id string) error {
	_, installed, dirName, err := m.repo.GetRuntimeState(ctx, id)
	if err != nil {
		return fmt.Errorf("load runtime state: %w", err)
	}
	if installed {
		return fmt.Errorf("plugin %s is already installed", id)
	}
	if dirName == "" {
		dirName = id
	}
	manifest, err := pluginsdk.LoadManifest(filepath.Join(m.dir, dirName, "manifest.toml"))
	if err != nil {
		return fmt.Errorf("load manifest: %w", err)
	}
	if err := m.repo.SetInstalled(ctx, id, true); err != nil {
		return err
	}
	if err := m.repo.SetEnabled(ctx, id, false); err != nil {
		return err
	}
	m.mu.Lock()
	if m.manifests == nil {
		m.manifests = make(map[string]*pluginsdk.Manifest)
	}
	m.manifests[id] = manifest
	m.mu.Unlock()
	return m.repo.UpdateStatus(ctx, id, StatusDisabled, "")
}

// IsInstalled reports whether the plugin is installed. A missing row counts
// as not installed.
func (m *Manager) IsInstalled(ctx context.Context, id string) (bool, error) {
	_, installed, _, err := m.repo.GetRuntimeState(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return installed, nil
}

// List returns the installed plugin rows with the manifest author and
// version history merged in (they are not persisted in plugin_instances).
func (m *Manager) List(ctx context.Context) ([]Instance, error) {
	list, err := m.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	m.mergeManifestMeta(list)
	return list, nil
}

// ListUninstalled returns the discovered-but-not-installed plugin rows
// (with author/history merged like List).
func (m *Manager) ListUninstalled(ctx context.Context) ([]Instance, error) {
	list, err := m.repo.ListUninstalled(ctx)
	if err != nil {
		return nil, err
	}
	m.mergeManifestMeta(list)
	return list, nil
}

func (m *Manager) mergeManifestMeta(list []Instance) {
	m.mu.Lock()
	for i := range list {
		if mf, ok := m.manifests[list[i].ID]; ok {
			list[i].Author = mf.Plugin.Author
			list[i].History = mf.Plugin.History
		}
	}
	m.mu.Unlock()
}

// logTailCount is the number of trailing log lines StreamLogs sends before
// following the file.
const logTailCount = 50

// logFollowInterval is how often StreamLogs checks the plugin log file for
// appended lines and rotation. A package var so tests can shrink it.
var logFollowInterval = time.Second

// recordStartRe matches the first line of a log record:
// "2026-09-11T18:05:57.013+08:00 I message". Lines that do not match are
// continuations of the current record (multi-line log messages).
var recordStartRe = regexp.MustCompile(`^20\d{2}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}[^ ]* [DIWE] `)

func isRecordStart(line string) bool { return recordStartRe.MatchString(line) }

// groupRecords joins physical lines into records: a record starts on every
// recordStartRe line and swallows all continuation lines after it.
func groupRecords(lines []string) []string {
	var records []string
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			records = append(records, strings.Join(cur, "\n"))
			cur = nil
		}
	}
	for _, l := range lines {
		if isRecordStart(l) && len(cur) > 0 {
			flush()
		}
		cur = append(cur, l)
	}
	flush()
	return records
}

// StreamOpts controls how StreamLogs starts the stream.
type StreamOpts struct {
	// Cursor is the timestamp of the last record the client already saw;
	// the stream resumes right after it (lines written during a
	// disconnect/pause are replayed). Empty = fresh open.
	Cursor string
	// FromEnd follows from the current end of the file (internal fallback
	// when a cursor cannot be located, e.g. after rotation).
	FromEnd bool
}

// StreamLogs tails a plugin's log file ({data_dir}/log/plugins/{name}.log):
// on a fresh open it emits the last logTailCount records' physical lines
// first (a leading incomplete record is dropped so the view always starts
// at a complete record); with a Cursor it replays from the FIRST line whose
// timestamp is >= cursor (the client dedupes the overlap against its
// buffer). Then it follows the file line by line. onGap is called once when
// the cursor could not be located (rotated away / recreated file) before
// following from the end. The frontend reassembles multi-line records from
// the lines. Rotation-safe: after lumberjack renames the file, the
// still-open descriptor is drained first (capturing the rest of the old
// file) before reopening the new path. Stops when ctx is cancelled or the
// file disappears.
func (m *Manager) StreamLogs(ctx context.Context, id string, opts StreamOpts, emit func(line string), onGap func()) error {
	path := logger.PluginLogPath(filepath.Dir(m.dir), id)
	if path == "" {
		return fmt.Errorf("invalid plugin name %q", id)
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open plugin log: %w", err)
	}
	// Defer through a closure: rotation reassigns f to the freshly opened
	// file, so the deferred call must close whatever f points to at return.
	defer func() { _ = f.Close() }()

	var partial []byte
	emitLines := func() {
		for {
			nl := -1
			for i := 0; i < len(partial); i++ {
				if partial[i] == '\n' {
					nl = i
					break
				}
			}
			if nl < 0 {
				break
			}
			if line := string(partial[:nl]); line != "" {
				emit(line)
			}
			partial = partial[nl+1:]
		}
	}

	switch {
	case opts.Cursor != "":
		pos, found := findCursorPos(f, opts.Cursor)
		if !found {
			onGap()
			if _, err := f.Seek(0, io.SeekEnd); err != nil {
				return fmt.Errorf("seek plugin log: %w", err)
			}
		} else {
			// Replay everything after the cursor line, then close the
			// read-write window: seek to the current end and drain again
			// so nothing between the replay and the follower is lost.
			if _, err := f.Seek(pos, io.SeekStart); err != nil {
				return fmt.Errorf("seek plugin log: %w", err)
			}
			if buf, err := io.ReadAll(f); err == nil && len(buf) > 0 {
				partial = append(partial, buf...)
				emitLines()
			}
			if _, err := f.Seek(0, io.SeekEnd); err != nil {
				return fmt.Errorf("seek plugin log: %w", err)
			}
			if buf, err := io.ReadAll(f); err == nil && len(buf) > 0 {
				partial = append(partial, buf...)
				emitLines()
			}
		}
	case opts.FromEnd:
		if _, err := f.Seek(0, io.SeekEnd); err != nil {
			return fmt.Errorf("seek plugin log: %w", err)
		}
	default:
		startSize := int64(0)
		if st, err := f.Stat(); err == nil {
			startSize = st.Size()
		}
		for _, rec := range tailRecords(f, logTailCount) {
			for _, line := range strings.Split(rec, "\n") {
				if line != "" {
					emit(line)
				}
			}
		}
		// Resume following from the size the tail was taken at: anything
		// appended afterwards arrives via the ticker.
		if _, err := f.Seek(startSize, io.SeekStart); err != nil {
			return fmt.Errorf("seek plugin log: %w", err)
		}
	}

	ticker := time.NewTicker(logFollowInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			buf, _ := io.ReadAll(f)
			if len(buf) > 0 {
				partial = append(partial, buf...)
				emitLines()
			}
			off, _ := f.Seek(0, io.SeekCurrent)
			fi, err1 := os.Stat(path)
			if err1 != nil {
				return fmt.Errorf("plugin log removed: %w", err1)
			}
			if ffi, err2 := f.Stat(); err2 == nil && !os.SameFile(fi, ffi) {
				// Rotation: drain completed above (old FD read to EOF),
				// then reopen the new file and catch up from its start.
				_ = f.Close()
				if f, err = os.Open(path); err != nil {
					return fmt.Errorf("reopen plugin log: %w", err)
				}
				if buf, err := io.ReadAll(f); err == nil && len(buf) > 0 {
					partial = append(partial, buf...)
					emitLines()
				}
				continue
			}
			if fi.Size() < off {
				// Truncated in place: restart from the beginning.
				if _, err := f.Seek(0, io.SeekStart); err == nil {
					buf, _ = io.ReadAll(f)
					if len(buf) > 0 {
						partial = append(partial, buf...)
						emitLines()
					}
				}
			}
		}
	}
}

// findCursorPos returns the byte offset of the FIRST record-start line
// whose timestamp is >= cursor, searching backwards up to 512KB (inclusive:
// replay starts AT the cursor timestamp, so same-timestamp lines are
// re-sent and the client dedupes). The match is only trusted when some
// record-start line before it within the window carries a timestamp <
// cursor (otherwise the true first occurrence lies outside the window and
// the caller must fall back to gap+FromEnd). Timestamps are compared as
// strings (ISO format, single offset).
func findCursorPos(f *os.File, cursor string) (int64, bool) {
	const chunk = 4096
	const maxScan = 512 * 1024
	st, err := f.Stat()
	if err != nil || st.Size() == 0 {
		return 0, false
	}
	off := st.Size()
	buf := make([]byte, 0, maxScan)
	for off > 0 && len(buf) < maxScan {
		read := int64(chunk)
		if off < read {
			read = off
		}
		off -= read
		chunkBuf := make([]byte, read)
		if _, err := f.ReadAt(chunkBuf, off); err != nil {
			break
		}
		buf = append(chunkBuf, buf...)
	}
	base := off
	lines := strings.Split(string(buf), "\n")
	offsets := make([]int64, 0, len(lines))
	pos := base
	for _, l := range lines {
		offsets = append(offsets, pos)
		pos += int64(len(l)) + 1
	}
	lineTS := func(l string) string { return strings.SplitN(l, " ", 3)[0] }

	// A leading fragment (scan started mid-file) is not a record start:
	// the window's first usable line is the first record-start line.
	first := 0
	if off > 0 {
		for first < len(lines) && !isRecordStart(lines[first]) {
			first++
		}
	}

	matchIdx := -1
	for i := first; i < len(lines); i++ {
		if !isRecordStart(lines[i]) {
			continue
		}
		if lineTS(lines[i]) >= cursor {
			matchIdx = i
			break
		}
	}
	if matchIdx < 0 {
		return 0, false
	}
	// Boundary proof: a record-start line with ts < cursor must precede
	// the match inside the window.
	for i := first; i < matchIdx; i++ {
		if isRecordStart(lines[i]) && lineTS(lines[i]) < cursor {
			return offsets[matchIdx], true
		}
	}
	return 0, false
}

// tailRecords reads up to n trailing records from the file via bounded
// backwards reads (512KB cap). A leading fragment from a mid-file scan
// start is dropped.
func tailRecords(f *os.File, n int) []string {
	const chunk = 4096
	const maxScan = 512 * 1024
	st, err := f.Stat()
	if err != nil || st.Size() == 0 {
		return nil
	}
	off := st.Size()
	buf := make([]byte, 0, maxScan)
	for off > 0 && len(buf) < maxScan {
		read := int64(chunk)
		if off < read {
			read = off
		}
		off -= read
		chunkBuf := make([]byte, read)
		if _, err := f.ReadAt(chunkBuf, off); err != nil {
			break
		}
		buf = append(chunkBuf, buf...)
		if off == 0 || countBytes(buf, '\n') > n {
			break
		}
	}
	lines := strings.Split(string(buf), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if off > 0 && len(lines) > 0 {
		// The first buffered line is a fragment from mid-file: drop
		// everything up to the next record start.
		i := 0
		for i < len(lines) && !isRecordStart(lines[i]) {
			i++
		}
		lines = lines[i:]
	}
	records := groupRecords(lines)
	if len(records) > n {
		records = records[len(records)-n:]
	}
	return records
}

func countBytes(b []byte, c byte) int {
	n := 0
	for _, x := range b {
		if x == c {
			n++
		}
	}
	return n
}

// SetConfig stores the plugin configuration and pushes the new values to
// the running plugin via LifecycleService.ConfigChanged (the SDK refreshes
// its cache and runs the plugin's optional ConfigChangeAware hook). The
// push is best-effort: when the plugin is not running or misses the call,
// it receives the values at the next Init.
func (m *Manager) SetConfig(ctx context.Context, id, configJSON string) error {
	if err := m.repo.SetConfig(ctx, id, configJSON); err != nil {
		return err
	}
	m.mu.Lock()
	rp, running := m.running[id]
	m.mu.Unlock()
	if !running || rp.lifecycle == nil {
		return nil
	}
	notifyCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := rp.lifecycle.ConfigChanged(notifyCtx, &gen.ConfigChangedRequest{
		ConfigJson: configJSON,
	}); err != nil {
		logger.Warn("[plugin] push config change to %s: %v", id, err)
	}
	return nil
}

// notifierHostPlugin is the host-side go-plugin adapter: it serves
// HostService on the broker and dispenses the Lifecycle + Task +
// NotifierService + UI clients.
type notifierHostPlugin struct {
	goplugin.NetRPCUnsupportedPlugin
	hs        *hostService
	brokerID  uint32
	lifecycle gen.LifecycleServiceClient
	tasks     gen.TaskServiceClient
	client    gen.NotifierServiceClient
	ui        gen.UIServiceClient
}

func (p *notifierHostPlugin) GRPCServer(broker *goplugin.GRPCBroker, s *grpc.Server) error {
	// Never called on the host side.
	return nil
}

func (p *notifierHostPlugin) GRPCClient(
	ctx context.Context,
	broker *goplugin.GRPCBroker,
	c *grpc.ClientConn,
) (interface{}, error) {
	id := broker.NextId()
	go broker.AcceptAndServe(id, func(opts []grpc.ServerOption) *grpc.Server {
		s := grpc.NewServer(opts...)
		gen.RegisterHostServiceServer(s, p.hs)
		return s
	})
	p.brokerID = id
	p.lifecycle = gen.NewLifecycleServiceClient(c)
	p.tasks = gen.NewTaskServiceClient(c)
	p.client = gen.NewNotifierServiceClient(c)
	p.ui = gen.NewUIServiceClient(c)
	return p, nil
}
