package config

import (
	"strings"
	"testing"
)

func TestValidateRejectsUnsafeProductionConfig(t *testing.T) {
	cfg := Config{
		Env:                    "production",
		StoreDriver:            "memory",
		CORSAllowedOrigins:     "*",
		TrustedProxyCIDRs:      "*",
		SecurityHeaders:        false,
		RateLimitEnabled:       false,
		DataEncryptionKey:      "change-me-32-plus-chars-random-secret",
		MetricsBearerToken:     "",
		BootstrapAdminPassword: "123456",
		BootstrapAgentPassword: "123456",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected production validation error")
	}
	message := err.Error()
	for _, want := range []string{"STORE_DRIVER", "DATABASE_URL", "REDIS_ADDR", "CORS_ALLOWED_ORIGINS", "ADMIN_BOOTSTRAP_PASSWORD"} {
		if !strings.Contains(message, want) {
			t.Fatalf("validation error missing %q: %s", want, message)
		}
	}
}

func TestValidateAcceptsProductionConfig(t *testing.T) {
	cfg := Config{
		Env:                    "production",
		StoreDriver:            "postgres",
		DatabaseURL:            "postgres://customer_service:secret@postgres:5432/customer_service?sslmode=disable",
		RedisAddr:              "redis:6379",
		CORSAllowedOrigins:     "https://service.example.com,https://admin.example.com",
		TrustedProxyCIDRs:      DefaultTrustedProxyCIDRs,
		SecurityHeaders:        true,
		RateLimitEnabled:       true,
		DataEncryptionKey:      "0123456789abcdef0123456789abcdef",
		MetricsBearerToken:     "metrics-token-012345678901",
		BootstrapAdminPassword: "AdminPass-0123456789",
		BootstrapAgentPassword: "AgentPass-0123456789",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}
