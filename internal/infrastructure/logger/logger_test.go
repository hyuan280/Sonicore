package logger

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenPluginLogWritesDedicatedFile(t *testing.T) {
	dataDir := t.TempDir()
	require.NoError(t, Init(Config{
		Level:      "debug",
		Format:     "console",
		DataDir:    dataDir,
		FileOutput: false,
		MaxSize:    10,
		MaxAge:     1,
		MaxBackups: 2,
	}))

	sl, closer, err := OpenPluginLog("demo")
	require.NoError(t, err)
	defer func() { _ = closer.Close() }()

	sl.Info("hello plugin")
	sl.Debug("trace debug")
	sl.Error("boom")

	data, err := os.ReadFile(filepath.Join(dataDir, "log", "plugins", "demo.log"))
	require.NoError(t, err)
	require.Contains(t, string(data), "hello plugin")
	require.Contains(t, string(data), "trace debug")
	require.Contains(t, string(data), "boom")
}

func TestOpenPluginLogInvalidName(t *testing.T) {
	require.NoError(t, Init(Config{Level: "info", DataDir: t.TempDir()}))

	_, _, err := OpenPluginLog("..")
	require.Error(t, err)
	_, _, err = OpenPluginLog(".")
	require.Error(t, err)
}

func TestPluginLogPath(t *testing.T) {
	require.Equal(t, "", PluginLogPath("/data", ".."))
	require.Equal(t, "", PluginLogPath("/data", "."))
	require.Equal(t, "", PluginLogPath("/data", ""))
	require.Equal(t, filepath.Join("/data", "log", "plugins", "demo.log"), PluginLogPath("/data", "demo"))
	require.Equal(t, filepath.Join("/data", "log", "plugins", "x.log"), PluginLogPath("/data", "../x"))
}
