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
