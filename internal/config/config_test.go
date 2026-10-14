package config

import (
	"strings"
	"testing"
	"time"
)

func TestAddr(t *testing.T) {
	cfg := Config{Host: "127.0.0.1", Port: 9000}

	if got := cfg.Addr(); got != "127.0.0.1:9000" {
		t.Errorf("Addr() = %q, want 127.0.0.1:9000", got)
	}
}

func TestValidateAcceptsThePortBounds(t *testing.T) {
	for _, port := range []int{1, 65535} {
		cfg := Config{Port: port}

		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() error for port %d = %v, want it accepted", port, err)
		}
	}
}

func TestValidateRejectsOutOfRangePorts(t *testing.T) {
	for _, port := range []int{0, -1, 65536} {
		cfg := Config{Port: port}

		err := cfg.Validate()
		if err == nil || !strings.Contains(err.Error(), "PORT") {
			t.Errorf("Validate() error for port %d = %v, want it to name PORT", port, err)
		}
	}
}

func TestLoadReadsTheEnvironment(t *testing.T) {
	t.Setenv("HOST", "127.0.0.1")
	t.Setenv("PORT", "9100")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("API_KEY", "secret")
	t.Setenv("CALDAV_SERVERS", "personal=https://cal.example.com/dav")
	t.Setenv("CACHE_PATH", "/tmp/caldav.db")
	t.Setenv("SYNC_INTERVAL_SECONDS", "120")
	t.Setenv("SYNC_LOOKAHEAD_DAYS", "30")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Addr() != "127.0.0.1:9100" {
		t.Errorf("Addr() = %q, want 127.0.0.1:9100", cfg.Addr())
	}

	if cfg.LogLevel != "debug" || cfg.APIKey != "secret" {
		t.Errorf("Load() = %+v, want the configured level and key", cfg)
	}

	if cfg.CalDAVServers != "personal=https://cal.example.com/dav" || cfg.CachePath != "/tmp/caldav.db" {
		t.Errorf("Load() = %+v, want the configured servers and cache path", cfg)
	}

	if cfg.SyncInterval() != 2*time.Minute {
		t.Errorf("SyncInterval() = %v, want 2m", cfg.SyncInterval())
	}

	if cfg.SyncLookahead() != 30*24*time.Hour {
		t.Errorf("SyncLookahead() = %v, want 720h", cfg.SyncLookahead())
	}
}

func TestLoadAppliesTheDefaults(t *testing.T) {
	t.Setenv("HOST", "")
	t.Setenv("PORT", "")
	t.Setenv("LOG_LEVEL", "")
	t.Setenv("API_KEY", "")
	t.Setenv("CALDAV_SERVERS", "")
	t.Setenv("CACHE_PATH", "")
	t.Setenv("SYNC_INTERVAL_SECONDS", "")
	t.Setenv("SYNC_LOOKAHEAD_DAYS", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Host != "0.0.0.0" || cfg.Port != 8080 || cfg.LogLevel != "info" || cfg.APIKey != "" {
		t.Errorf("Load() = %+v, want the documented defaults", cfg)
	}

	if cfg.CachePath != "/data/caldav.db" {
		t.Errorf("CachePath = %q, want /data/caldav.db", cfg.CachePath)
	}

	if cfg.SyncInterval() != time.Hour {
		t.Errorf("SyncInterval() = %v, want 1h", cfg.SyncInterval())
	}

	if cfg.SyncLookahead() != 60*24*time.Hour {
		t.Errorf("SyncLookahead() = %v, want 1440h", cfg.SyncLookahead())
	}
}

func TestValidateRejectsNegativeSyncSettings(t *testing.T) {
	tests := map[string]Config{
		"interval":  {Port: 8080, SyncIntervalSeconds: -1},
		"lookahead": {Port: 8080, SyncLookaheadDays: -1},
	}

	for name, cfg := range tests {
		t.Run(name, func(t *testing.T) {
			if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "SYNC_") {
				t.Errorf("Validate() error = %v, want it to name the SYNC_ variable", err)
			}
		})
	}
}

func TestSyncIntervalIsOffAtZero(t *testing.T) {
	if got := (Config{}).SyncInterval(); got != 0 {
		t.Errorf("SyncInterval() = %v, want 0 so an external timer can drive the fetch", got)
	}
}

func TestSyncLookaheadFallsBackToSixtyDays(t *testing.T) {
	if got := (Config{}).SyncLookahead(); got != 60*24*time.Hour {
		t.Errorf("SyncLookahead() = %v, want 1440h", got)
	}
}

func TestLoadRejectsANonNumericPort(t *testing.T) {
	t.Setenv("PORT", "http")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want the malformed port rejected")
	}
}
