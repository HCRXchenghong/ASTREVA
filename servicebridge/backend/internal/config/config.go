package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

const DefaultTrustedProxyCIDRs = "127.0.0.1/32,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,100.64.0.0/10,fc00::/7,fe80::/10"

type Config struct {
	HTTPAddr           string
	Env                string
	LogLevel           slog.Level
	CORSAllowedOrigins string
	TrustedProxyCIDRs  string
	SecurityHeaders    bool
	RateLimitEnabled   bool
	RateLimitRPS       float64
	RateLimitBurst     int
	MetricsBearerToken string

	OpenAIAPIKey      string
	OpenAIBaseURL     string
	OpenAIModel       string
	OpenAIAPIType     string
	DataEncryptionKey string

	BootstrapAdminPassword string
	BootstrapAgentPassword string

	StoreDriver  string
	DatabaseURL  string
	RedisAddr    string
	RedisChannel string
	NodeID       string

	UploadDriver        string
	UploadDir           string
	UploadPublicBaseURL string
	UploadMaxBytes      int64
	S3Endpoint          string
	S3Region            string
	S3Bucket            string
	S3AccessKeyID       string
	S3SecretAccessKey   string
	S3SessionToken      string
	S3ForcePathStyle    bool
	S3KeyPrefix         string
	S3PublicBaseURL     string

	PushWebhookURL            string
	PushWebhookBearerToken    string
	PushWebhookTimeoutSeconds int
}

func Load() Config {
	return Config{
		HTTPAddr:                  env("HTTP_ADDR", ":8080"),
		Env:                       env("APP_ENV", "local"),
		LogLevel:                  parseLogLevel(env("LOG_LEVEL", "info")),
		CORSAllowedOrigins:        env("CORS_ALLOWED_ORIGINS", "*"),
		TrustedProxyCIDRs:         env("TRUSTED_PROXY_CIDRS", DefaultTrustedProxyCIDRs),
		SecurityHeaders:           envBool("SECURITY_HEADERS", true),
		RateLimitEnabled:          envBool("RATE_LIMIT_ENABLED", true),
		RateLimitRPS:              envFloat64("RATE_LIMIT_RPS", 20),
		RateLimitBurst:            envInt("RATE_LIMIT_BURST", 60),
		MetricsBearerToken:        env("METRICS_BEARER_TOKEN", ""),
		OpenAIAPIKey:              env("OPENAI_API_KEY", ""),
		OpenAIBaseURL:             env("OPENAI_BASE_URL", "https://api.openai.com/v1"),
		OpenAIModel:               env("OPENAI_MODEL", "gpt-4o-mini"),
		OpenAIAPIType:             env("OPENAI_API_TYPE", "chat_completions"),
		DataEncryptionKey:         env("DATA_ENCRYPTION_KEY", ""),
		BootstrapAdminPassword:    env("ADMIN_BOOTSTRAP_PASSWORD", "123456"),
		BootstrapAgentPassword:    env("AGENT_BOOTSTRAP_PASSWORD", "123456"),
		StoreDriver:               env("STORE_DRIVER", "memory"),
		DatabaseURL:               env("DATABASE_URL", ""),
		RedisAddr:                 env("REDIS_ADDR", ""),
		RedisChannel:              env("REDIS_CHANNEL", "customer-service:ws-events"),
		NodeID:                    env("NODE_ID", defaultNodeID()),
		UploadDriver:              env("UPLOAD_DRIVER", "local"),
		UploadDir:                 env("UPLOAD_DIR", "uploads"),
		UploadPublicBaseURL:       env("UPLOAD_PUBLIC_BASE_URL", ""),
		UploadMaxBytes:            envInt64("UPLOAD_MAX_BYTES", 10*1024*1024),
		S3Endpoint:                env("S3_ENDPOINT", ""),
		S3Region:                  env("S3_REGION", "us-east-1"),
		S3Bucket:                  env("S3_BUCKET", ""),
		S3AccessKeyID:             env("S3_ACCESS_KEY_ID", ""),
		S3SecretAccessKey:         env("S3_SECRET_ACCESS_KEY", ""),
		S3SessionToken:            env("S3_SESSION_TOKEN", ""),
		S3ForcePathStyle:          envBool("S3_FORCE_PATH_STYLE", false),
		S3KeyPrefix:               env("S3_KEY_PREFIX", "uploads"),
		S3PublicBaseURL:           env("S3_PUBLIC_BASE_URL", ""),
		PushWebhookURL:            env("PUSH_WEBHOOK_URL", ""),
		PushWebhookBearerToken:    env("PUSH_WEBHOOK_BEARER_TOKEN", ""),
		PushWebhookTimeoutSeconds: envInt("PUSH_WEBHOOK_TIMEOUT_SECONDS", 5),
	}
}

func (c Config) Validate() error {
	if !isProduction(c.Env) {
		return nil
	}
	var problems []string
	driver := strings.ToLower(strings.TrimSpace(c.StoreDriver))
	if driver != "postgres" {
		problems = append(problems, "STORE_DRIVER must be postgres in production")
	}
	if strings.TrimSpace(c.DatabaseURL) == "" {
		problems = append(problems, "DATABASE_URL is required in production")
	}
	if strings.TrimSpace(c.RedisAddr) == "" {
		problems = append(problems, "REDIS_ADDR is required in production for multi-node websocket delivery")
	}
	if !strongSecret(c.DataEncryptionKey, 32) {
		problems = append(problems, "DATA_ENCRYPTION_KEY must be at least 32 chars and not a placeholder")
	}
	if !strongSecret(c.MetricsBearerToken, 24) {
		problems = append(problems, "METRICS_BEARER_TOKEN must be set in production")
	}
	if !strongPassword(c.BootstrapAdminPassword) {
		problems = append(problems, "ADMIN_BOOTSTRAP_PASSWORD must be set to a non-default password with at least 12 chars")
	}
	if !strongPassword(c.BootstrapAgentPassword) {
		problems = append(problems, "AGENT_BOOTSTRAP_PASSWORD must be set to a non-default password with at least 12 chars")
	}
	if !c.SecurityHeaders {
		problems = append(problems, "SECURITY_HEADERS must be true in production")
	}
	if !c.RateLimitEnabled {
		problems = append(problems, "RATE_LIMIT_ENABLED must be true in production")
	}
	origins := strings.TrimSpace(c.CORSAllowedOrigins)
	if origins == "" || origins == "*" {
		problems = append(problems, "CORS_ALLOWED_ORIGINS must list production origins, not *")
	}
	if strings.TrimSpace(c.TrustedProxyCIDRs) == "" || strings.TrimSpace(c.TrustedProxyCIDRs) == "*" {
		problems = append(problems, "TRUSTED_PROXY_CIDRS must list trusted proxy CIDRs, not *")
	}
	if strings.EqualFold(strings.TrimSpace(c.UploadDriver), "s3") && strings.TrimSpace(c.S3Bucket) == "" {
		problems = append(problems, "S3_BUCKET is required when UPLOAD_DRIVER=s3")
	}
	if len(problems) > 0 {
		return fmt.Errorf("production configuration is unsafe: %s", strings.Join(problems, "; "))
	}
	return nil
}

func isProduction(envName string) bool {
	envName = strings.ToLower(strings.TrimSpace(envName))
	return envName == "prod" || envName == "production"
}

func strongSecret(value string, minLen int) bool {
	value = strings.TrimSpace(value)
	lower := strings.ToLower(value)
	if len(value) < minLen {
		return false
	}
	if strings.Contains(lower, "change-me") || strings.Contains(lower, "changeme") || strings.Contains(lower, "placeholder") || strings.Contains(lower, "secret") {
		return false
	}
	return true
}

func strongPassword(value string) bool {
	value = strings.TrimSpace(value)
	lower := strings.ToLower(value)
	if len(value) < 12 {
		return false
	}
	if value == "123456" || strings.Contains(lower, "change-me") || strings.Contains(lower, "changeme") || strings.Contains(lower, "password") {
		return false
	}
	return true
}

func env(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func parseLogLevel(value string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func envInt64(key string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func envBool(key string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if value == "" {
		return fallback
	}
	switch value {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return fallback
	}
}

func envFloat64(key string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func defaultNodeID() string {
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		hostname = "node"
	}
	return hostname + "-" + time.Now().UTC().Format("20060102150405")
}
