package plugin

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatHCLogArgs(t *testing.T) {
	require.Equal(t, "hello", formatHCLogArgs("hello"))
	require.Equal(t, "hello k=v n=1", formatHCLogArgs("hello", "k", "v", "n", 1))
	require.Equal(t, "hello k=<missing>", formatHCLogArgs("hello", "k"))
	require.Equal(t, "msg a=1 b=two", formatHCLogArgs("msg", "a", 1, "b", "two"))
}

func TestHCLogAdapterWithAndNamed(t *testing.T) {
	buf := &bytes.Buffer{}
	sl := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	a := newHCLogAdapter("x", sl).(*hclogAdapter)

	// With returns a derived logger carrying the implied args; the parent
	// stays untouched.
	child := a.With("k", "v").(*hclogAdapter)
	require.Len(t, child.ImpliedArgs(), 2)
	require.Nil(t, a.ImpliedArgs())

	child.Info("hello")
	// The base logger's emit carries the owner identity even though
	// Name() stays "plugin".
	require.Contains(t, buf.String(), "[plugin:x] hello k=v")

	// Named appends the child name with ":"; ResetNamed resets. The base
	// name is just "plugin" — the plugin name arrives via go-plugin's
	// logStderr derivation (binary basename = plugin name).
	named := a.Named("sub").(*hclogAdapter)
	require.Equal(t, "plugin:sub", named.Name())
	require.Equal(t, "plugin", a.Name())
	buf.Reset()
	named.Info("m2")
	require.Contains(t, buf.String(), "[plugin:sub] m2")
	require.NotContains(t, buf.String(), "[plugin] m2")

	reset := named.ResetNamed("other").(*hclogAdapter)
	require.Equal(t, "other", reset.Name())
	require.Equal(t, "plugin:sub", named.Name())

	// Chained Named keeps appending (plugin:demo:x) and inherits the sink
	// flag.
	chained := a.Named("a").(*hclogAdapter).Named("b").(*hclogAdapter)
	require.Equal(t, "plugin:a:b", chained.Name())
	require.True(t, chained.sinkPlugin)

	// The logStderr derivation (binary basename = plugin name) turns the
	// base "plugin" into "plugin:x" — no dedupe needed. Those forwarded
	// plugin-stderr lines are host-side machinery, so the sink flips.
	same := a.Named("x").(*hclogAdapter)
	require.Equal(t, "plugin:x", same.Name())
	require.False(t, same.sinkPlugin)
}

func TestHCLogAdapterInternalNamesSkipPluginSink(t *testing.T) {
	buf := &bytes.Buffer{}
	sl := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	a := newHCLogAdapter("x", sl).(*hclogAdapter)

	// go-plugin internals (stdio EOF noise, handshake socket paths) go to
	// the host logger, never into the plugin's own sink.
	a.Named("stdio").Debug("received EOF, stopping recv loop", "err", "EOF")
	a.Named("stdio").Info("copystderr done")
	a.Named("plugin").Debug("plugin address", "address", "/tmp/plugin123", "network", "unix")
	require.Empty(t, buf.String())

	// The host-routing flag is inherited by further derivation from an
	// internal logger; internal lines are displayed as
	// "plugin:<sub>:<name>" so they can't be confused with plugin logs.
	internal := a.Named("stdio").(*hclogAdapter)
	require.False(t, internal.sinkPlugin)
	require.Equal(t, "plugin:stdio:x", internal.Name())
	derived := internal.Named("y").(*hclogAdapter)
	require.False(t, derived.sinkPlugin)
	require.Equal(t, "plugin:stdio:x:y", derived.Name())
	derived.Info("still host-side")
	require.Empty(t, buf.String())

	// Regular plugin-side lines still reach the sink.
	a.Debug("keep me")
	require.Contains(t, buf.String(), "keep me")
}

func TestSanitizeLogMessage(t *testing.T) {
	require.Equal(t, "plain", sanitizeLogMessage("plain"))
	require.Equal(t, "a\nb", sanitizeLogMessage("a\nb"))
	require.Equal(t, "a\nb", sanitizeLogMessage("a\r\nb"))
	require.Equal(t, "a\nb", sanitizeLogMessage("a\rb"))
	// Control characters (ESC/ANSI, C0) are stripped; \n and \t survive.
	require.Equal(t, "a[31mb\nc", sanitizeLogMessage("a\x1b[31mb\r\n\x01c"))
	require.Equal(t, "a\tb", sanitizeLogMessage("a\tb"))
}

func TestHCLogAdapterMultilinePassThrough(t *testing.T) {
	buf := &bytes.Buffer{}
	sl := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	a := newHCLogAdapter("x", sl)

	a.Info("first\nsecond")
	require.Contains(t, buf.String(), "first")
	require.Contains(t, buf.String(), "second")
}

func TestHCLogAdapterOddImpliedArg(t *testing.T) {
	buf := &bytes.Buffer{}
	sl := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	a := newHCLogAdapter("x", sl)

	a.With("only").Error("boom")
	require.Contains(t, buf.String(), "boom only=<missing>")
}
