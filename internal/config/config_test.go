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
