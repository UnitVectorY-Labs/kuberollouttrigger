package config

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/redis/go-redis/v9"
)

// CommonConfig holds configuration shared between web and worker modes.
type CommonConfig struct {
	LogLevel     string
	ValkeyAddr   string
	ValkeyChannel string
	ValkeyUsername string
	ValkeyPassword string
	ValkeyTLS    bool
}

// WebConfig holds configuration specific to the web mode.
type WebConfig struct {
	CommonConfig
	ListenAddr        string
	GithubOIDCAudience string
	GithubAllowedOrg  string
	AllowedImagePrefix string
	// DevMode disables OIDC signature verification for local development.
	DevMode bool
}

// WorkerConfig holds configuration specific to the worker mode.
type WorkerConfig struct {
	CommonConfig
	AllowedImagePrefix string
	Kubeconfig         string
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func envBool(key string) bool {
	v := os.Getenv(key)
	return strings.EqualFold(v, "true") || v == "1"
}

// ParseWebConfig parses web mode configuration from env vars and CLI flags.
func ParseWebConfig(args []string) (*WebConfig, error) {
	fs := flag.NewFlagSet("web", flag.ContinueOnError)

	cfg := &WebConfig{}
	fs.StringVar(&cfg.LogLevel, "log-level", envOrDefault("LOG_LEVEL", "info"), "Log level (debug, info, warn, error)")
	fs.StringVar(&cfg.ValkeyAddr, "valkey-addr", envOrDefault("VALKEY_ADDR", ""), "Valkey address (host:port)")
	fs.StringVar(&cfg.ValkeyChannel, "valkey-channel", envOrDefault("VALKEY_CHANNEL", "kuberollouttrigger"), "Valkey PubSub channel")
	fs.StringVar(&cfg.ValkeyUsername, "valkey-username", envOrDefault("VALKEY_USERNAME", ""), "Valkey username")
	fs.StringVar(&cfg.ValkeyPassword, "valkey-password", envOrDefault("VALKEY_PASSWORD", ""), "Valkey password")
	fs.BoolVar(&cfg.ValkeyTLS, "valkey-tls", envBool("VALKEY_TLS_ENABLED"), "Enable TLS for Valkey")

	fs.StringVar(&cfg.ListenAddr, "listen-addr", envOrDefault("WEB_LISTEN_ADDR", ":8080"), "HTTP listen address")
	fs.StringVar(&cfg.GithubOIDCAudience, "github-oidc-audience", envOrDefault("GITHUB_OIDC_AUDIENCE", ""), "Required OIDC audience")
	fs.StringVar(&cfg.GithubAllowedOrg, "github-allowed-org", envOrDefault("GITHUB_ALLOWED_ORG", ""), "Allowed GitHub organization")
	fs.StringVar(&cfg.AllowedImagePrefix, "allowed-image-prefix", envOrDefault("ALLOWED_IMAGE_PREFIX", ""), "Allowed image prefix")
	fs.BoolVar(&cfg.DevMode, "dev-mode", envBool("DEV_MODE"), "Enable dev mode (disables OIDC signature verification)")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	// Validate required fields
	var missing []string
	if cfg.ValkeyAddr == "" {
		missing = append(missing, "VALKEY_ADDR / --valkey-addr")
	}
	if cfg.GithubOIDCAudience == "" {
		missing = append(missing, "GITHUB_OIDC_AUDIENCE / --github-oidc-audience")
	}
	if cfg.GithubAllowedOrg == "" {
		missing = append(missing, "GITHUB_ALLOWED_ORG / --github-allowed-org")
	}
	if cfg.AllowedImagePrefix == "" {
		missing = append(missing, "ALLOWED_IMAGE_PREFIX / --allowed-image-prefix")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required configuration: %s", strings.Join(missing, ", "))
	}

	return cfg, nil
}

// ParseWorkerConfig parses worker mode configuration from env vars and CLI flags.
func ParseWorkerConfig(args []string) (*WorkerConfig, error) {
	fs := flag.NewFlagSet("worker", flag.ContinueOnError)

	cfg := &WorkerConfig{}
	fs.StringVar(&cfg.LogLevel, "log-level", envOrDefault("LOG_LEVEL", "info"), "Log level (debug, info, warn, error)")
	fs.StringVar(&cfg.ValkeyAddr, "valkey-addr", envOrDefault("VALKEY_ADDR", ""), "Valkey address (host:port)")
	fs.StringVar(&cfg.ValkeyChannel, "valkey-channel", envOrDefault("VALKEY_CHANNEL", "kuberollouttrigger"), "Valkey PubSub channel")
	fs.StringVar(&cfg.ValkeyUsername, "valkey-username", envOrDefault("VALKEY_USERNAME", ""), "Valkey username")
	fs.StringVar(&cfg.ValkeyPassword, "valkey-password", envOrDefault("VALKEY_PASSWORD", ""), "Valkey password")
	fs.BoolVar(&cfg.ValkeyTLS, "valkey-tls", envBool("VALKEY_TLS_ENABLED"), "Enable TLS for Valkey")

	fs.StringVar(&cfg.AllowedImagePrefix, "allowed-image-prefix", envOrDefault("ALLOWED_IMAGE_PREFIX", ""), "Allowed image prefix")
	fs.StringVar(&cfg.Kubeconfig, "kubeconfig", envOrDefault("KUBECONFIG", ""), "Path to kubeconfig file (empty for in-cluster)")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	// Validate required fields
	var missing []string
	if cfg.ValkeyAddr == "" {
		missing = append(missing, "VALKEY_ADDR / --valkey-addr")
	}
	if cfg.AllowedImagePrefix == "" {
		missing = append(missing, "ALLOWED_IMAGE_PREFIX / --allowed-image-prefix")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required configuration: %s", strings.Join(missing, ", "))
	}

	return cfg, nil
}

// ParseLogLevel converts a log level string to slog.Level.
func ParseLogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
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

// NewRedisOptions creates redis.Options from the common configuration.
func (c *CommonConfig) NewRedisOptions() *redis.Options {
	opts := &redis.Options{
		Addr:     c.ValkeyAddr,
		Username: c.ValkeyUsername,
		Password: c.ValkeyPassword,
	}
	if c.ValkeyTLS {
		opts.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}
	return opts
}

// LogSummary logs the configuration summary, redacting secrets.
func (c *WebConfig) LogSummary(logger *slog.Logger) {
	logger.Info("web mode configuration",
		"listen_addr", c.ListenAddr,
		"valkey_addr", c.ValkeyAddr,
		"valkey_channel", c.ValkeyChannel,
		"valkey_tls", c.ValkeyTLS,
		"github_oidc_audience", c.GithubOIDCAudience,
		"github_allowed_org", c.GithubAllowedOrg,
		"allowed_image_prefix", c.AllowedImagePrefix,
		"dev_mode", c.DevMode,
		"log_level", c.LogLevel,
	)
}

// LogSummary logs the configuration summary, redacting secrets.
func (c *WorkerConfig) LogSummary(logger *slog.Logger) {
	kubeconfig := c.Kubeconfig
	if kubeconfig == "" {
		kubeconfig = "(in-cluster)"
	}
	logger.Info("worker mode configuration",
		"valkey_addr", c.ValkeyAddr,
		"valkey_channel", c.ValkeyChannel,
		"valkey_tls", c.ValkeyTLS,
		"allowed_image_prefix", c.AllowedImagePrefix,
		"kubeconfig", kubeconfig,
		"log_level", c.LogLevel,
	)
}
