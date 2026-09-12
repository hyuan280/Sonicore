package plugin

import (
	"fmt"
	"io"
	"log"
	"log/slog"
	"strings"

	"github.com/hashicorp/go-hclog"

	"github.com/sonicore/server/internal/infrastructure/logger"
)

// hclogAdapter routes go-plugin's internal hclog output through the project
// logger (or a plugin's own slog.Logger) so plugin lifecycle messages use
// the same console format (timestamp + level letter + [module] prefix) as
// everything else. Level filtering is left to the global log level, so
// go-plugin debug lines become D-level lines (visible when log.level =
// "debug").
type hclogAdapter struct {
	displayName string
	// owner is the plugin name; internal (host-routed) loggers display
	// it so host-log lines keep their per-plugin context.
	owner   string
	level   hclog.Level
	implied []interface{}
	slog    *slog.Logger
	// sinkPlugin routes this logger's output into the plugin's dedicated
	// log file (false = host logger only). Decided once at derivation
	// (Named/ResetNamed) for go-plugin's internal sub-loggers; emit only
	// reads the flag, never matches on the name string.
	sinkPlugin bool
}

// newHCLogAdapter creates the plugin client's logger: the base display
// name is just "plugin" — go-plugin's own logStderr derivation
// (Named(filepath.Base(binary path))) appends the binary name, which
// equals the plugin name by deployment convention, producing
// "plugin:<name>" (and "plugin:<name>:<sub>" for any further derivation).
func newHCLogAdapter(pluginName string, sl *slog.Logger) hclog.Logger {
	return &hclogAdapter{
		displayName: "plugin",
		owner:       pluginName,
		level:       hclog.Debug,
		slog:        sl,
		sinkPlugin:  true,
	}
}

// internalSubName reports whether a Named/ResetNamed argument refers to
// one of go-plugin's fixed internal loggers: their output is host-side
// machinery and must never enter the plugin log file.
//
//	go-plugin v1.6.3 derives exactly: "stdio" (grpc_client.go, always) and
//	"yamux" (grpcmux muxers, only with ProtocolGRPCMux — we use plain
//	GRPC, listed here defensively). The logStderr derivation
//	(client.go) uses filepath.Base(binary path); because the binary is
//	named after the plugin, that equals the plugin name and is deduped in
//	Named instead. "plugin" stays for backward compatibility with
//	directories still containing a binary called "plugin".
func internalSubName(name string) bool {
	return name == "stdio" || name == "plugin" || name == "yamux"
}

func (a *hclogAdapter) emit(level hclog.Level, msg string, args ...interface{}) {
	if level < a.level {
		return
	}
	// Implied args (from With) come first, then the call args.
	combined := make([]interface{}, 0, len(a.implied)+len(args))
	combined = append(combined, a.implied...)
	combined = append(combined, args...)
	text := formatHCLogArgs(msg, combined...)
	text = sanitizeLogMessage(text)
	// The base logger (no derivation) carries go-plugin's host-side
	// client lines ("starting plugin", "plugin process exited", ...):
	// display the plugin's identity so multi-plugin hosts can tell who
	// they belong to.
	name := a.displayName
	if name == "plugin" && a.owner != "" {
		name = "plugin:" + a.owner
	}
	line := fmt.Sprintf("[%s] %s", name, text)
	// go-plugin's internal machinery (stdio EOF noise, handshake socket
	// paths) is host-side information: it belongs in the host log
	// (sonicore.log/console, honoring the global level), never in the
	// plugin's dedicated log file.
	usePluginSink := a.slog != nil && a.sinkPlugin
	if usePluginSink {
		switch level {
		case hclog.Trace, hclog.Debug:
			a.slog.Debug(line)
		case hclog.Info:
			a.slog.Info(line)
		case hclog.Warn:
			a.slog.Warn(line)
		case hclog.Error:
			a.slog.Error(line)
		}
		return
	}
	switch level {
	case hclog.Trace, hclog.Debug:
		logger.Debug("%s", line)
	case hclog.Info:
		logger.Info("%s", line)
	case hclog.Warn:
		logger.Warn("%s", line)
	case hclog.Error:
		logger.Error("%s", line)
	}
}

// formatHCLogArgs appends the hclog-style key/value pairs to the message.
func formatHCLogArgs(msg string, args ...interface{}) string {
	if len(args) == 0 {
		return msg
	}
	var b strings.Builder
	b.WriteString(msg)
	for i := 0; i < len(args); i += 2 {
		b.WriteString(" ")
		b.WriteString(fmt.Sprint(args[i]))
		b.WriteString("=")
		if i+1 < len(args) {
			b.WriteString(fmt.Sprint(args[i+1]))
		} else {
			b.WriteString("<missing>")
		}
	}
	return b.String()
}

func (a *hclogAdapter) Trace(msg string, args ...interface{}) { a.emit(hclog.Trace, msg, args...) }
func (a *hclogAdapter) Debug(msg string, args ...interface{}) { a.emit(hclog.Debug, msg, args...) }
func (a *hclogAdapter) Info(msg string, args ...interface{})  { a.emit(hclog.Info, msg, args...) }
func (a *hclogAdapter) Warn(msg string, args ...interface{})  { a.emit(hclog.Warn, msg, args...) }
func (a *hclogAdapter) Error(msg string, args ...interface{}) { a.emit(hclog.Error, msg, args...) }
func (a *hclogAdapter) Log(level hclog.Level, msg string, args ...interface{}) {
	a.emit(level, msg, args...)
}

func (a *hclogAdapter) IsTrace() bool { return a.level <= hclog.Trace }
func (a *hclogAdapter) IsDebug() bool { return a.level <= hclog.Debug }
func (a *hclogAdapter) IsInfo() bool  { return a.level <= hclog.Info }
func (a *hclogAdapter) IsWarn() bool  { return a.level <= hclog.Warn }
func (a *hclogAdapter) IsError() bool { return a.level <= hclog.Error }

// ImpliedArgs exposes the key/value pairs attached via With, per the
// hclog.Logger contract.
func (a *hclogAdapter) ImpliedArgs() []interface{} { return a.implied }

// With returns a derived logger carrying the extra key/value pairs instead
// of silently dropping them.
func (a *hclogAdapter) With(args ...interface{}) hclog.Logger {
	na := *a
	na.implied = make([]interface{}, 0, len(a.implied)+len(args))
	na.implied = append(na.implied, a.implied...)
	na.implied = append(na.implied, args...)
	return &na
}

func (a *hclogAdapter) Name() string { return a.displayName }

// Named returns a derived logger with the child name appended using ":"
// (parent:child, so go-plugin's logStderr derivation of the binary name
// turns the base "plugin" into "plugin:<name>"; any further derivation
// reads "plugin:<name>:<sub>"). Two derivations are host-side machinery
// and flip the sink to the host logger: go-plugin's fixed internal
// loggers ("stdio"/"yamux" — displayed as "plugin:<sub>:<name>" so their
// lines can never be confused with plugin-side logs) and the logStderr
// derivation itself (binary basename == plugin name — forwarded
// plugin-stderr lines like handshake socket paths, which must not enter
// the plugin log file). The flag is inherited by any further derivation.
func (a *hclogAdapter) Named(name string) hclog.Logger {
	na := *a
	if internalSubName(name) {
		na.sinkPlugin = false
		na.displayName = "plugin:" + name + ":" + na.owner
		return &na
	}
	if name == na.owner {
		na.sinkPlugin = false
	}
	na.displayName = a.displayName + ":" + name
	return &na
}

// ResetNamed returns a derived logger whose name is reset to the given one
// (dropping the parent chain), per the hclog contract.
func (a *hclogAdapter) ResetNamed(name string) hclog.Logger {
	na := *a
	na.displayName = name
	if internalSubName(name) {
		na.sinkPlugin = false
	}
	return &na
}

func (a *hclogAdapter) SetLevel(level hclog.Level) { a.level = level }

func (a *hclogAdapter) GetLevel() hclog.Level { return a.level }

// StandardLogger is deliberately a discard sink: by design the host reads
// only the go-plugin handshake line from the plugin's stdout — anything
// else the process prints is ignored. Plugins must report their own
// diagnostics through the SDK (Log/LogError/ReportStatus); the SDK's Run
// recovers panics and reports them that way, so nothing valuable is lost
// here.
func (a *hclogAdapter) StandardLogger(opts *hclog.StandardLoggerOptions) *log.Logger {
	return log.New(io.Discard, "", 0)
}

// StandardWriter returns a discard writer for the same reason as
// StandardLogger: the plugin's stdout/stderr serves the handshake only.
// go-plugin's logStdout/logStderr line forwarding is intentionally not
// routed into the plugin log — unstructured duplicate output would pollute
// the dedicated per-plugin file. Crashes and diagnostics reach the host
// via the SDK's Log/ReportStatus (panic recovery in Run).
func (a *hclogAdapter) StandardWriter(opts *hclog.StandardLoggerOptions) io.Writer {
	return io.Discard
}

var _ hclog.Logger = (*hclogAdapter)(nil)
