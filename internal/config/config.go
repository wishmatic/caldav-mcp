package config

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

const defaultSyncLookaheadDays = 60

type Config struct {
	Host string `env:"HOST" envDefault:"0.0.0.0"`
	Port int    `env:"PORT" envDefault:"8080"`

	LogLevel string `env:"LOG_LEVEL" envDefault:"info"`

	APIKey string `env:"API_KEY"`

	// CalDAVServers is the name=url,name=url list of servers to fetch from.
	CalDAVServers string `env:"CALDAV_SERVERS"`

	CachePath string `env:"CACHE_PATH" envDefault:"/data/caldav.db"`

	// SyncIntervalSeconds is how often events are fetched. Zero turns the
	// periodic fetch off, for deployments that run `server sync` on a timer of
	// their own.
	SyncIntervalSeconds int `env:"SYNC_INTERVAL_SECONDS" envDefault:"3600"`

	// SyncLookaheadDays is how far ahead events are fetched, and so the horizon
	// the get_events tool can answer for.
	SyncLookaheadDays int `env:"SYNC_LOOKAHEAD_DAYS" envDefault:"60"`
}

func Load() (Config, error) {
	_ = godotenv.Load()

	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse environment: %w", err)
	}

	return cfg, nil
}

func (c Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

func (c Config) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("PORT must be between 1 and 65535, got %d", c.Port)
	}

	if c.SyncIntervalSeconds < 0 {
		return fmt.Errorf("SYNC_INTERVAL_SECONDS must not be negative, got %d", c.SyncIntervalSeconds)
	}

	if c.SyncLookaheadDays < 0 {
		return fmt.Errorf("SYNC_LOOKAHEAD_DAYS must not be negative, got %d", c.SyncLookaheadDays)
	}

	return nil
}

// SyncInterval is how often events are fetched. Zero means the periodic fetch
// is off.
func (c Config) SyncInterval() time.Duration {
	if c.SyncIntervalSeconds <= 0 {
		return 0
	}

	return time.Duration(c.SyncIntervalSeconds) * time.Second
}

// SyncLookahead is how far ahead events are fetched.
func (c Config) SyncLookahead() time.Duration {
	days := c.SyncLookaheadDays
	if days <= 0 {
		days = defaultSyncLookaheadDays
	}

	return time.Duration(days) * 24 * time.Hour
}
