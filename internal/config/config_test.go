package config

import (
	"testing"
	"time"
)

func TestValidateRejectsInvalidCollectorConcurrency(t *testing.T) {
	cfg := &Config{
		Web:     WebConfig{Addr: ":7001", JWTSecret: "secret", TokenTTL: time.Hour},
		Storage: StorageConfig{DSN: "edge.db"},
		Collector: CollectorConfig{
			MaxConcurrency: 0,
			ReadTimeout:    time.Second,
			WriteTimeout:   time.Second,
			ReconnectDelay: time.Second,
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid max concurrency to be rejected")
	}
}

func TestValidateRejectsDefaultCredentialsInProduction(t *testing.T) {
	cfg := &Config{
		Web: WebConfig{
			Addr:            ":7001",
			Production:      true,
			JWTSecret:       "jetlinks-edge-default-secret-change-me",
			DefaultPassword: "admin123",
			TokenTTL:        time.Hour,
		},
		Storage: StorageConfig{DSN: "edge.db"},
		Collector: CollectorConfig{
			MaxConcurrency: 1,
			ReadTimeout:    time.Second,
			WriteTimeout:   time.Second,
			ReconnectDelay: time.Second,
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected production mode to reject default credentials")
	}
	cfg.Web.JWTSecret = "production-secret"
	cfg.Web.DefaultPassword = "changed-password"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected production config error: %v", err)
	}
}

func TestValidateTrustedProxies(t *testing.T) {
	cfg := &Config{
		Web:       WebConfig{Addr: "0.0.0.0:7001", JWTSecret: "secret", DefaultPassword: "secret", TokenTTL: time.Hour, TrustedProxies: []string{"not-a-network"}},
		Storage:   StorageConfig{DSN: "edge.db"},
		Collector: CollectorConfig{MaxConcurrency: 1, ReadTimeout: time.Second, WriteTimeout: time.Second, ReconnectDelay: time.Second},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid trusted proxy to be rejected")
	}
	cfg.Web.TrustedProxies = []string{"127.0.0.1", "10.0.0.0/8"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid trusted proxies rejected: %v", err)
	}
}
