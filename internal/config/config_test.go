package config

import (
	"strings"
	"testing"
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
}

func TestLoadAppliesTheDefaults(t *testing.T) {
	t.Setenv("HOST", "")
	t.Setenv("PORT", "")
	t.Setenv("LOG_LEVEL", "")
	t.Setenv("API_KEY", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Host != "0.0.0.0" || cfg.Port != 8080 || cfg.LogLevel != "info" || cfg.APIKey != "" {
		t.Errorf("Load() = %+v, want the documented defaults", cfg)
	}
}

func TestLoadRejectsANonNumericPort(t *testing.T) {
	t.Setenv("PORT", "http")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want the malformed port rejected")
	}
}
