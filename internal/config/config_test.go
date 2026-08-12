package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDatabaseConfigDSN(t *testing.T) {
	cfg := DatabaseConfig{
		Host:     "db.example.com",
		Port:     5432,
		User:     "sonicore",
		Password: "s3cret",
		DBName:   "music",
		SSLMode:  "require",
	}
	assert.Equal(t,
		"host=db.example.com port=5432 user=sonicore password=s3cret dbname=music sslmode=require",
		cfg.DSN())
}

func TestDatabaseConfigDSNDefaults(t *testing.T) {
	cfg := DatabaseConfig{Host: "localhost", Port: 15432, User: "u", DBName: "d"}
	assert.Equal(t,
		"host=localhost port=15432 user=u password= dbname=d sslmode=",
		cfg.DSN())
}

func TestRedisConfigAddr(t *testing.T) {
	tests := []struct {
		name string
		cfg  RedisConfig
		want string
	}{
		{"default port", RedisConfig{Host: "localhost", Port: 6379}, "localhost:6379"},
		{"custom port", RedisConfig{Host: "redis.internal", Port: 16380}, "redis.internal:16380"},
		{"empty host", RedisConfig{Port: 6379}, ":6379"},
		{"zero port", RedisConfig{Host: "localhost"}, "localhost:0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.cfg.Addr())
		})
	}
}

func TestConfigInitDirs(t *testing.T) {
	base := t.TempDir()
	cfg := &Config{
		Data: DataConfig{
			DataDir:   filepath.Join(base, "data"),
			ImagesDir: filepath.Join(base, "data", "images"),
			CacheDir:  filepath.Join(base, "data", "cache"),
			LyricsDir: filepath.Join(base, "data", "lyrics"),
		},
	}

	require.NoError(t, cfg.InitDirs())
	for _, dir := range []string{cfg.Data.DataDir, cfg.Data.ImagesDir, cfg.Data.CacheDir, cfg.Data.LyricsDir} {
		info, err := os.Stat(dir)
		require.NoError(t, err, "dir %s should exist", dir)
		assert.True(t, info.IsDir(), "%s should be a directory", dir)
	}
}

func TestConfigInitDirsIdempotent(t *testing.T) {
	base := t.TempDir()
	cfg := &Config{
		Data: DataConfig{
			DataDir:   filepath.Join(base, "data"),
			ImagesDir: filepath.Join(base, "data", "images"),
			CacheDir:  filepath.Join(base, "data", "cache"),
			LyricsDir: filepath.Join(base, "data", "lyrics"),
		},
	}

	require.NoError(t, cfg.InitDirs())
	require.NoError(t, cfg.InitDirs())
}

func TestConfigInitDirsFailure(t *testing.T) {
	base := t.TempDir()
	blockingFile := filepath.Join(base, "blocked")
	require.NoError(t, os.WriteFile(blockingFile, []byte("x"), 0644))

	cfg := &Config{
		Data: DataConfig{
			DataDir:   filepath.Join(blockingFile, "data"),
			ImagesDir: filepath.Join(base, "ok"),
		},
	}

	err := cfg.InitDirs()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create directories")

	_, statErr := os.Stat(filepath.Join(base, "ok"))
	assert.NoError(t, statErr, "unaffected dirs should still be created")
}
