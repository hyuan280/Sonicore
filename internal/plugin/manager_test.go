package plugin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	pluginsdk "github.com/hyuan280/Sonicore-PluginSDK/go"
	"github.com/hyuan280/Sonicore-PluginSDK/go/gen"

	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/infrastructure/task"
)

func TestDefaultConfigJSON(t *testing.T) {
	m := &pluginsdk.Manifest{Plugin: pluginsdk.PluginInfo{
		Config: map[string]pluginsdk.ConfigField{
			"greeting": {Key: "greeting", Type: "string", Default: "Hi"},
			"level":    {Key: "level", Type: "string"},
		},
	}}
	require.JSONEq(t, `{"greeting":"Hi"}`, defaultConfigJSON(m))

	require.Equal(t, "", defaultConfigJSON(&pluginsdk.Manifest{}))
	require.Equal(t, "", defaultConfigJSON(&pluginsdk.Manifest{Plugin: pluginsdk.PluginInfo{
		Config: map[string]pluginsdk.ConfigField{"a": {Key: "a", Type: "string"}},
	}}))
}

func TestPluginTaskID(t *testing.T) {
	require.Equal(t, "plugin:demo:t1", pluginTaskID("demo", "t1"))
}

func TestConfigSchemaSorted(t *testing.T) {
	m := &Manager{manifests: map[string]*pluginsdk.Manifest{
		"demo": {Plugin: pluginsdk.PluginInfo{Config: map[string]pluginsdk.ConfigField{
			"b": {Key: "b", Type: "string"},
			"a": {Key: "a", Type: "number"},
		}}},
	}}
	fields, ok := m.ConfigSchema("demo")
	require.True(t, ok)
	require.Len(t, fields, 2)
	require.Equal(t, "a", fields[0].Key)
	require.Equal(t, "b", fields[1].Key)

	_, ok = m.ConfigSchema("nope")
	require.False(t, ok)
}

func TestManagerGetConfig(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	m := &Manager{repo: NewRepo(db)}
	ctx := context.Background()

	mock.ExpectQuery("SELECT config FROM plugin_config").
		WithArgs("demo").
		WillReturnRows(sqlmock.NewRows([]string{"config"}).AddRow([]byte(`{"greeting":"Hi"}`)))
	cfg, err := m.GetConfig(ctx, "demo")
	require.NoError(t, err)
	require.Equal(t, map[string]any{"greeting": "Hi"}, cfg)

	mock.ExpectQuery("SELECT config FROM plugin_config").
		WithArgs("demo").
		WillReturnRows(sqlmock.NewRows([]string{"config"}).AddRow([]byte(`oops`)))
	_, err = m.GetConfig(ctx, "demo")
	require.ErrorContains(t, err, "parse plugin config")

	mock.ExpectQuery("SELECT config FROM plugin_config").
		WithArgs("demo").
		WillReturnRows(sqlmock.NewRows([]string{"config"}))
	cfg, err = m.GetConfig(ctx, "demo")
	require.NoError(t, err)
	require.Empty(t, cfg)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestExitReason(t *testing.T) {
	require.Equal(t, "", exitReason(nil))
	require.Equal(t, "", exitReason(&exec.Cmd{}))

	cmd := exec.Command("sh", "-c", "exit 3")
	require.NoError(t, cmd.Start())
	require.Error(t, cmd.Wait())
	require.Equal(t, " (exit code 3)", exitReason(cmd))

	cmd = exec.Command("sleep", "5")
	require.NoError(t, cmd.Start())
	require.NoError(t, cmd.Process.Kill())
	_ = cmd.Wait()
	require.Equal(t, " (signal: killed)", exitReason(cmd))
}

// ---- schedule registration against the real task.Scheduler ----

type fakeTaskStore struct{ enabled map[string]bool }

func (f *fakeTaskStore) GetEnabled(id string) (bool, error) { return f.enabled[id], nil }
func (f *fakeTaskStore) SetEnabled(id string, enabled bool) error {
	f.enabled[id] = enabled
	return nil
}

type fakeTaskClient struct {
	calls []string
	resp  *gen.RunTaskResponse
	err   error
}

func (f *fakeTaskClient) RunTask(
	ctx context.Context,
	in *gen.RunTaskRequest,
	opts ...grpc.CallOption,
) (*gen.RunTaskResponse, error) {
	f.calls = append(f.calls, in.TaskId)
	if f.err != nil {
		return nil, f.err
	}
	if f.resp != nil {
		return f.resp, nil
	}
	return &gen.RunTaskResponse{}, nil
}

func newTestScheduler(t *testing.T) *task.Scheduler {
	t.Helper()
	return task.NewScheduler(&fakeTaskStore{enabled: map[string]bool{}})
}

func TestRegisterScheduleValidation(t *testing.T) {
	m := &Manager{}
	err := m.registerSchedule("demo", &gen.ScheduleSpec{Id: "t1", Interval: "30s"}, &fakeTaskClient{})
	require.ErrorContains(t, err, "scheduler unavailable")

	m = &Manager{sched: newTestScheduler(t)}

	err = m.registerSchedule("demo", &gen.ScheduleSpec{Id: "t1", Interval: "abc"}, &fakeTaskClient{})
	require.ErrorContains(t, err, "invalid interval")

	err = m.registerSchedule("demo", &gen.ScheduleSpec{Id: "t1", Interval: "500ms"}, &fakeTaskClient{})
	require.ErrorContains(t, err, "at least 1s")

	err = m.registerSchedule("demo", &gen.ScheduleSpec{Id: "t1", Cron: "not-a-cron"}, &fakeTaskClient{})
	require.ErrorContains(t, err, "invalid cron")
}

func TestRegisterScheduleIntervalRunsPluginTask(t *testing.T) {
	m := &Manager{sched: newTestScheduler(t)}
	client := &fakeTaskClient{}
	err := m.registerSchedule("demo", &gen.ScheduleSpec{Id: "t1", Name: "心跳", Interval: "30s"}, client)
	require.NoError(t, err)

	tasks := m.sched.List()
	require.Len(t, tasks, 1)
	require.Equal(t, "plugin:demo:t1", tasks[0].ID)
	require.Equal(t, "plugin", tasks[0].Source)
	require.Equal(t, "demo", tasks[0].Provider)
	require.Equal(t, "心跳", tasks[0].Name)
	require.EqualValues(t, 30, tasks[0].IntervalSec)

	require.NoError(t, m.sched.RunNow("plugin:demo:t1"))
	require.Eventually(t, func() bool { return len(client.calls) == 1 }, time.Second, 10*time.Millisecond)
	require.Equal(t, "t1", client.calls[0])
}

func TestRemovePluginTasks(t *testing.T) {
	m := &Manager{sched: newTestScheduler(t)}
	require.NoError(t, m.registerSchedule("demo", &gen.ScheduleSpec{Id: "t1", Interval: "30s"}, &fakeTaskClient{}))
	require.NoError(t, m.registerSchedule("other", &gen.ScheduleSpec{Id: "t2", Interval: "30s"}, &fakeTaskClient{}))

	m.removePluginTasks("demo")

	tasks := m.sched.List()
	require.Len(t, tasks, 1)
	require.Equal(t, "plugin:other:t2", tasks[0].ID)
}

// ---- has_page probing ----

type fakeUIClient struct {
	form, page string
	pageErr    error
}

func (f *fakeUIClient) GetForm(
	ctx context.Context,
	in *gen.GetFormRequest,
	opts ...grpc.CallOption,
) (*gen.GetFormResponse, error) {
	return &gen.GetFormResponse{SchemaJson: f.form}, nil
}

func (f *fakeUIClient) GetPage(
	ctx context.Context,
	in *gen.GetPageRequest,
	opts ...grpc.CallOption,
) (*gen.GetPageResponse, error) {
	if f.pageErr != nil {
		return nil, f.pageErr
	}
	return &gen.GetPageResponse{SchemaJson: f.page}, nil
}

func TestRefreshHasPage(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()

	m := &Manager{
		repo:    NewRepo(db),
		running: map[string]*runningPlugin{"demo": {ui: &fakeUIClient{page: `{"a":1}`}}},
	}
	mock.ExpectExec("UPDATE plugin_instances SET has_page").
		WithArgs("demo", true).
		WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, m.refreshHasPage(ctx, "demo"))

	m2 := &Manager{
		repo:    NewRepo(db),
		running: map[string]*runningPlugin{"demo": {ui: &fakeUIClient{}}},
	}
	mock.ExpectExec("UPDATE plugin_instances SET has_page").
		WithArgs("demo", false).
		WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, m2.refreshHasPage(ctx, "demo"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func writeTestManifest(t *testing.T, dir, name string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	content := fmt.Sprintf(`[plugin]
name = %q
version = "1.0.0"
description = "test plugin"
services = ["notifier"]
abi = "sonicore.plugin.v1"
`, name)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.toml"), []byte(content), 0o644))
}

func TestStartDoubleLaunchGuard(t *testing.T) {
	m := &Manager{
		dir:      t.TempDir(),
		running:  map[string]*runningPlugin{},
		starting: map[string]bool{"demo": true},
	}
	err := m.start(context.Background(), m.dir, &pluginsdk.Manifest{Plugin: pluginsdk.PluginInfo{Name: "demo"}})
	require.EqualError(t, err, "already starting")

	m.starting["demo"] = false
	m.running["demo"] = &runningPlugin{}
	err = m.start(context.Background(), m.dir, &pluginsdk.Manifest{Plugin: pluginsdk.PluginInfo{Name: "demo"}})
	require.EqualError(t, err, "already running")
}

func TestManagerIsInstalled(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	m := &Manager{repo: NewRepo(db)}
	ctx := context.Background()

	mock.ExpectQuery("SELECT enabled, installed, dir FROM plugin_instances").
		WithArgs("demo").
		WillReturnRows(sqlmock.NewRows([]string{"enabled", "installed", "dir"}).AddRow(true, true, "demo"))
	installed, err := m.IsInstalled(ctx, "demo")
	require.NoError(t, err)
	require.True(t, installed)

	// Missing row counts as not installed (new discovery).
	mock.ExpectQuery("SELECT enabled, installed, dir FROM plugin_instances").
		WithArgs("nope").
		WillReturnRows(sqlmock.NewRows([]string{"enabled", "installed", "dir"}))
	installed, err = m.IsInstalled(ctx, "nope")
	require.NoError(t, err)
	require.False(t, installed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestManagerInstall(t *testing.T) {
	root := t.TempDir()
	writeTestManifest(t, filepath.Join(root, "demo"), "demo")
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	m := &Manager{dir: root, repo: NewRepo(db)}
	ctx := context.Background()

	mock.ExpectQuery("SELECT enabled, installed, dir FROM plugin_instances").
		WithArgs("demo").
		WillReturnRows(sqlmock.NewRows([]string{"enabled", "installed", "dir"}).AddRow(true, false, "demo"))
	mock.ExpectExec("UPDATE plugin_instances SET installed").
		WithArgs("demo", true).
		WillReturnResult(sqlmock.NewResult(0, 1))
	// Installs default to disabled.
	mock.ExpectExec("UPDATE plugin_instances SET enabled").
		WithArgs("demo", false).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE plugin_instances SET status").
		WithArgs("demo", StatusDisabled, "").
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, m.Install(ctx, "demo"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestManagerInstallAlreadyInstalled(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	m := &Manager{dir: t.TempDir(), repo: NewRepo(db)}

	mock.ExpectQuery("SELECT enabled, installed, dir FROM plugin_instances").
		WithArgs("demo").
		WillReturnRows(sqlmock.NewRows([]string{"enabled", "installed", "dir"}).AddRow(true, true, "demo"))

	err = m.Install(context.Background(), "demo")
	require.ErrorContains(t, err, "already installed")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestManagerUninstallSoft(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	var unregistered []string
	m := &Manager{
		dir:  t.TempDir(),
		repo: NewRepo(db),
		manifests: map[string]*pluginsdk.Manifest{
			"demo": {Plugin: pluginsdk.PluginInfo{Services: []string{"notifier"}}},
		},
		unregister: func(ct domain.ChannelType) { unregistered = append(unregistered, string(ct)) },
	}
	ctx := context.Background()

	mock.ExpectQuery("SELECT enabled, installed, dir FROM plugin_instances").
		WithArgs("demo").
		WillReturnRows(sqlmock.NewRows([]string{"enabled", "installed", "dir"}).AddRow(true, true, "demo"))
	mock.ExpectExec("UPDATE plugin_instances SET has_page").
		WithArgs("demo", false).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE plugin_instances SET status").
		WithArgs("demo", StatusStopped, "").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE plugin_instances SET installed").
		WithArgs("demo", false).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, m.Uninstall(ctx, "demo"))
	require.Equal(t, []string{"demo"}, unregistered)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestManagerDeleteRemovesDirAndRow(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "demo")
	writeTestManifest(t, pluginDir, "demo")
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	m := &Manager{dir: root, repo: NewRepo(db)}

	mock.ExpectQuery("SELECT enabled, installed, dir FROM plugin_instances").
		WithArgs("demo").
		WillReturnRows(sqlmock.NewRows([]string{"enabled", "installed", "dir"}).AddRow(true, false, "demo"))
	mock.ExpectExec("DELETE FROM plugin_instances").
		WithArgs("demo").
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, m.Delete(context.Background(), "demo"))
	_, err = os.Stat(pluginDir)
	require.True(t, os.IsNotExist(err))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestManagerDeleteRefusesEmptyDir(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	m := &Manager{dir: t.TempDir(), repo: NewRepo(db)}

	for _, dirName := range []string{"", ".", "..", "a/b", "/etc"} {
		mock.ExpectQuery("SELECT enabled, installed, dir FROM plugin_instances").
			WithArgs("demo").
			WillReturnRows(sqlmock.NewRows([]string{"enabled", "installed", "dir"}).AddRow(true, false, dirName))

		err = m.Delete(context.Background(), "demo")
		require.ErrorContains(t, err, "refusing to delete")
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

func tsLine(i int) string {
	return fmt.Sprintf("2026-01-01T00:%02d:%02d.000Z I line %03d", i/60, i%60, i)
}

func TestTailRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.log")
	var b strings.Builder
	for i := 1; i <= 120; i++ {
		fmt.Fprintf(&b, "%s\n", tsLine(i))
	}
	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o644))

	fh, err := os.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = fh.Close() })

	records := tailRecords(fh, 50)
	require.Len(t, records, 50)
	require.Equal(t, tsLine(71), records[0])
	require.Equal(t, tsLine(120), records[49])
}

func TestTailRecordsMultiline(t *testing.T) {
	content := tsLine(1) + "\ncont-a\ncont-b\n" + tsLine(2) + "\n"
	path := filepath.Join(t.TempDir(), "p.log")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	fh, err := os.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = fh.Close() })

	records := tailRecords(fh, 50)
	require.Equal(t, []string{
		tsLine(1) + "\ncont-a\ncont-b",
		tsLine(2),
	}, records)
}

func TestStreamLogsAppendedLineArrivesWithoutFollowup(t *testing.T) {
	old := logFollowInterval
	logFollowInterval = 20 * time.Millisecond
	t.Cleanup(func() { logFollowInterval = old })

	root := t.TempDir()
	logDir := filepath.Join(root, "log", "plugins")
	require.NoError(t, os.MkdirAll(logDir, 0o755))
	path := filepath.Join(logDir, "demo.log")
	require.NoError(t, os.WriteFile(path, []byte(tsLine(1)+"\n"), 0o644))

	m := &Manager{dir: filepath.Join(root, "plugins")}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var got []string
	go func() {
		_ = m.StreamLogs(ctx, "demo", StreamOpts{}, func(line string) {
			mu.Lock()
			got = append(got, line)
			mu.Unlock()
		}, nil)
	}()

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) == 1
	}, time.Second, 10*time.Millisecond)

	// A single appended line with nothing after it must still arrive —
	// lines are emitted as soon as they are written, no record batching.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	_, err = f.WriteString(tsLine(2) + "\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) >= 2
	}, time.Second, 10*time.Millisecond)
	require.Equal(t, tsLine(2), got[1])
}

func TestStreamLogsFollowAndRotation(t *testing.T) {
	old := logFollowInterval
	logFollowInterval = 20 * time.Millisecond
	t.Cleanup(func() { logFollowInterval = old })

	root := t.TempDir()
	logDir := filepath.Join(root, "log", "plugins")
	require.NoError(t, os.MkdirAll(logDir, 0o755))
	path := filepath.Join(logDir, "demo.log")
	require.NoError(t, os.WriteFile(path, []byte(tsLine(1)+"\n"+tsLine(2)+"\n"), 0o644))

	m := &Manager{dir: filepath.Join(root, "plugins")}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var got []string
	done := make(chan struct{})
	go func() {
		_ = m.StreamLogs(ctx, "demo", StreamOpts{}, func(line string) {
			mu.Lock()
			got = append(got, line)
			mu.Unlock()
		}, nil)
		close(done)
	}()

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) == 2
	}, time.Second, 10*time.Millisecond)
	require.Equal(t, []string{tsLine(1), tsLine(2)}, got)

	// Appended lines arrive through the follower one by one — the
	// frontend reassembles the multi-line record from ts3 + cont.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	_, err = f.WriteString(tsLine(3) + "\ncont\n" + tsLine(4) + "\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) >= 4
	}, time.Second, 10*time.Millisecond)
	require.Equal(t, tsLine(3), got[2])
	require.Equal(t, "cont", got[3])

	// Rotation: rename the old file away and create a fresh one — the
	// follower must drain the old FD and pick up the new file.
	require.NoError(t, os.Rename(path, path+".bak"))
	require.NoError(t, os.WriteFile(path, []byte(tsLine(5)+"\n"), 0o644))
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, l := range got {
			if l == tsLine(5) {
				return true
			}
		}
		return false
	}, time.Second, 10*time.Millisecond)

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stream did not stop after cancel")
	}
}

func TestStreamLogsMissingFile(t *testing.T) {
	m := &Manager{dir: filepath.Join(t.TempDir(), "plugins")}
	var emitted int
	err := m.StreamLogs(context.Background(), "ghost", StreamOpts{}, func(line string) { emitted++ }, nil)
	require.ErrorContains(t, err, "open plugin log")
	require.Equal(t, 0, emitted)

	err = m.StreamLogs(context.Background(), "..", StreamOpts{}, func(line string) { emitted++ }, nil)
	require.ErrorContains(t, err, "invalid plugin name")
}

func TestStreamLogsFollowOnlySkipsTail(t *testing.T) {
	old := logFollowInterval
	logFollowInterval = 20 * time.Millisecond
	t.Cleanup(func() { logFollowInterval = old })

	root := t.TempDir()
	logDir := filepath.Join(root, "log", "plugins")
	require.NoError(t, os.MkdirAll(logDir, 0o755))
	path := filepath.Join(logDir, "demo.log")
	require.NoError(t, os.WriteFile(path, []byte(tsLine(1)+"\n"+tsLine(2)+"\n"), 0o644))

	m := &Manager{dir: filepath.Join(root, "plugins")}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var got []string
	go func() {
		_ = m.StreamLogs(ctx, "demo", StreamOpts{FromEnd: true}, func(line string) {
			mu.Lock()
			got = append(got, line)
			mu.Unlock()
		}, nil)
	}()

	// The existing tail must NOT be replayed.
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	require.Empty(t, got)
	mu.Unlock()

	// But appended lines still arrive.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	_, err = f.WriteString(tsLine(3) + "\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) == 1
	}, time.Second, 10*time.Millisecond)
	require.Equal(t, tsLine(3), got[0])
}

func TestFindCursorPos(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.log")
	content := tsLine(1) + "\n" + tsLine(2) + "\n" + tsLine(3) + "\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	fh, err := os.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = fh.Close() })

	// Cursor = ts of line 2 → replay starts AT line 2 (first occurrence,
	// inclusive).
	pos, ok := findCursorPos(fh, strings.Fields(tsLine(2))[0])
	require.True(t, ok)
	require.Equal(t, int64(len(tsLine(1)+"\n")), pos)

	// Cursor older than everything → not found.
	_, ok = findCursorPos(fh, "2000-01-01T00:00:00.000Z")
	require.False(t, ok)
}

func TestFindCursorPosSameTimestampGroup(t *testing.T) {
	ts := strings.Fields(tsLine(2))[0]
	content := tsLine(1) + "\n" + ts + " I first\n" + ts + " I second\n" + tsLine(3) + "\n"
	path := filepath.Join(t.TempDir(), "p.log")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	fh, err := os.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = fh.Close() })

	// The cursor group starts at the FIRST line carrying that timestamp.
	pos, ok := findCursorPos(fh, ts)
	require.True(t, ok)
	require.Equal(t, int64(len(tsLine(1)+"\n")), pos)
}

func TestStreamLogsResumeFromCursor(t *testing.T) {
	old := logFollowInterval
	logFollowInterval = 20 * time.Millisecond
	t.Cleanup(func() { logFollowInterval = old })

	root := t.TempDir()
	logDir := filepath.Join(root, "log", "plugins")
	require.NoError(t, os.MkdirAll(logDir, 0o755))
	path := filepath.Join(logDir, "demo.log")
	require.NoError(t, os.WriteFile(path, []byte(
		tsLine(1)+"\n"+tsLine(2)+"\n"+tsLine(3)+"\n"+tsLine(4)+"\n"), 0o644))

	m := &Manager{dir: filepath.Join(root, "plugins")}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var got []string
	gaps := 0
	// The client already saw lines 1-2; resume from the cursor (ts of
	// line 2) → replay starts AT line 2 (inclusive, the client dedupes
	// the overlap), so lines 2-4 arrive.
	go func() {
		_ = m.StreamLogs(ctx, "demo", StreamOpts{Cursor: strings.Fields(tsLine(2))[0]}, func(line string) {
			mu.Lock()
			got = append(got, line)
			mu.Unlock()
		}, func() { gaps++ })
	}()

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) == 3
	}, time.Second, 10*time.Millisecond)
	require.Equal(t, []string{tsLine(2), tsLine(3), tsLine(4)}, got)
	require.Equal(t, 0, gaps)

	// Appended lines still arrive afterwards.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	_, err = f.WriteString(tsLine(5) + "\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) >= 4
	}, time.Second, 10*time.Millisecond)
	require.Equal(t, tsLine(5), got[3])
}

func TestStreamLogsCursorNotFoundGap(t *testing.T) {
	old := logFollowInterval
	logFollowInterval = 20 * time.Millisecond
	t.Cleanup(func() { logFollowInterval = old })

	root := t.TempDir()
	logDir := filepath.Join(root, "log", "plugins")
	require.NoError(t, os.MkdirAll(logDir, 0o755))
	path := filepath.Join(logDir, "demo.log")
	require.NoError(t, os.WriteFile(path, []byte(tsLine(1)+"\n"+tsLine(2)+"\n"), 0o644))

	m := &Manager{dir: filepath.Join(root, "plugins")}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var got []string
	gaps := 0
	// Cursor older than every line in the file → gap + follow from end
	// (existing lines must NOT be replayed).
	go func() {
		_ = m.StreamLogs(ctx, "demo", StreamOpts{Cursor: "2000-01-01T00:00:00.000Z"}, func(line string) {
			mu.Lock()
			got = append(got, line)
			mu.Unlock()
		}, func() { gaps++ })
	}()

	require.Eventually(t, func() bool { return gaps == 1 }, time.Second, 10*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	require.Empty(t, got)
	mu.Unlock()

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	_, err = f.WriteString(tsLine(3) + "\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) == 1
	}, time.Second, 10*time.Millisecond)
	require.Equal(t, tsLine(3), got[0])
}

func TestStreamLogsCursorAtWindowStartGap(t *testing.T) {
	old := logFollowInterval
	logFollowInterval = 20 * time.Millisecond
	t.Cleanup(func() { logFollowInterval = old })

	root := t.TempDir()
	logDir := filepath.Join(root, "log", "plugins")
	require.NoError(t, os.MkdirAll(logDir, 0o755))
	path := filepath.Join(logDir, "demo.log")
	require.NoError(t, os.WriteFile(path, []byte(tsLine(1)+"\n"+tsLine(2)+"\n"), 0o644))

	m := &Manager{dir: filepath.Join(root, "plugins")}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var got []string
	gaps := 0
	// Cursor = ts of the window's FIRST line: the boundary proof fails
	// (nothing older before it) → gap + follow from end.
	go func() {
		_ = m.StreamLogs(ctx, "demo", StreamOpts{Cursor: strings.Fields(tsLine(1))[0]}, func(line string) {
			mu.Lock()
			got = append(got, line)
			mu.Unlock()
		}, func() { gaps++ })
	}()

	require.Eventually(t, func() bool { return gaps == 1 }, time.Second, 10*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	require.Empty(t, got)
	mu.Unlock()

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	_, err = f.WriteString(tsLine(3) + "\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) == 1
	}, time.Second, 10*time.Millisecond)
	require.Equal(t, tsLine(3), got[0])
}
