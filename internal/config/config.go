package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	Redis    RedisConfig    `mapstructure:"redis"`
	Data     DataConfig     `mapstructure:"data"`
	JWT      JWTConfig      `mapstructure:"jwt"`
	Log      LogConfig      `mapstructure:"log"`
	Audio    AudioConfig    `mapstructure:"audio"`
	Metadata MetadataConfig `mapstructure:"metadata"`
}

type MetadataConfig struct {
	MusicBrainzEnabled    bool   `mapstructure:"musicbrainz_enabled"`
	MusicBrainzAPIURL     string `mapstructure:"musicbrainz_api_url"`
	MusicBrainzRateLimit  int    `mapstructure:"musicbrainz_rate_limit"`
	MusicBrainzAppName    string `mapstructure:"musicbrainz_app_name"`
	MusicBrainzAppVersion string `mapstructure:"musicbrainz_app_version"`
}

type ServerConfig struct {
	Host    string `mapstructure:"host"`
	Port    int    `mapstructure:"port"`
	BaseURL string `mapstructure:"base_url"`
	WebDir  string `mapstructure:"web_dir"`
}

type DatabaseConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	DBName   string `mapstructure:"dbname"`
	SSLMode  string `mapstructure:"sslmode"`
}

type RedisConfig struct {
	Host      string `mapstructure:"host"`
	Port      int    `mapstructure:"port"`
	Password  string `mapstructure:"password"`
	DB        int    `mapstructure:"db"`
	KeyPrefix string `mapstructure:"key_prefix"`
}

type DataConfig struct {
	MusicDir  string `mapstructure:"music_dir"`
	DataDir   string `mapstructure:"data_dir"`
	ImagesDir string `mapstructure:"images_dir"`
	CacheDir  string `mapstructure:"cache_dir"`
}

type JWTConfig struct {
	Secret          string `mapstructure:"secret"`
	Expiration      string `mapstructure:"expiration"`
	RefreshExpiration string `mapstructure:"refresh_expiration"`
}

type AudioConfig struct {
	PulseServer string `mapstructure:"pulse_server"`
}

type LogConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		d.Host, d.Port, d.User, d.Password, d.DBName, d.SSLMode,
	)
}

func (r RedisConfig) Addr() string {
	return fmt.Sprintf("%s:%d", r.Host, r.Port)
}

func Load() *Config {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("toml")
	v.AddConfigPath(".")
	v.AddConfigPath("/etc/sonicore")
	v.AddConfigPath("$HOME/.sonicore")
	v.AddConfigPath("/opt/opencode/work/Sonicore")

	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", 4530)
	v.SetDefault("server.base_url", "http://localhost:4530")
	v.SetDefault("server.web_dir", "web/dist")
	v.SetDefault("database.host", "localhost")
	v.SetDefault("database.port", 5432)
	v.SetDefault("database.user", "sonicore")
	v.SetDefault("database.password", "sonicore")
	v.SetDefault("database.dbname", "sonicore")
	v.SetDefault("database.sslmode", "disable")
	v.SetDefault("redis.host", "localhost")
	v.SetDefault("redis.port", 6379)
	v.SetDefault("redis.password", "")
	v.SetDefault("redis.db", 0)
	v.SetDefault("redis.key_prefix", "sonicore:")
	v.SetDefault("data.music_dir", "/opt/sonicore/music")
	v.SetDefault("data.data_dir", "/opt/sonicore/data")
	v.SetDefault("data.images_dir", "/opt/sonicore/data/images")
	v.SetDefault("data.cache_dir", "/opt/sonicore/data/cache")
	v.SetDefault("jwt.secret", "change-me-in-production")
	v.SetDefault("jwt.expiration", "24h")
	v.SetDefault("jwt.refresh_expiration", "720h")
	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "console")

	v.SetDefault("metadata.musicbrainz_enabled", false)
	v.SetDefault("metadata.musicbrainz_api_url", "https://musicbrainz.org/ws/2")
	v.SetDefault("metadata.musicbrainz_rate_limit", 1)
	v.SetDefault("metadata.musicbrainz_app_name", "Sonicore")
	v.SetDefault("metadata.musicbrainz_app_version", "0.1.0")

	v.AutomaticEnv()
	v.SetEnvPrefix("SONICORE")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			fmt.Fprintf(os.Stderr, "warning: config error: %v\n", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		fmt.Fprintf(os.Stderr, "warning: config unmarshal error: %v\n", err)
	}

	return &cfg
}

func (c *Config) InitDirs() error {
	dirs := []string{c.Data.DataDir, c.Data.ImagesDir, c.Data.CacheDir}
	var errs []string
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", dir, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("failed to create directories: %s", strings.Join(errs, "; "))
	}
	return nil
}
