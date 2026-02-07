package config

import (
	"os"
	"testing"
)

func TestParseWebConfig_Defaults(t *testing.T) {
	cfg, err := ParseWebConfig([]string{
		"--valkey-addr", "localhost:6379",
		"--github-oidc-audience", "test-aud",
		"--github-allowed-org", "test-org",
		"--allowed-image-prefix", "ghcr.io/test/",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ListenAddr != ":8080" {
		t.Errorf("expected default listen addr :8080, got %s", cfg.ListenAddr)
	}
	if cfg.ValkeyChannel != "kuberollouttrigger" {
		t.Errorf("expected default channel kuberollouttrigger, got %s", cfg.ValkeyChannel)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("expected default log level info, got %s", cfg.LogLevel)
	}
	if cfg.DevMode {
		t.Error("expected dev mode to be false by default")
	}
}

func TestParseWebConfig_MissingRequired(t *testing.T) {
	_, err := ParseWebConfig([]string{})
	if err == nil {
		t.Fatal("expected error for missing required config")
	}
}

func TestParseWebConfig_MissingValkeyAddr(t *testing.T) {
	_, err := ParseWebConfig([]string{
		"--github-oidc-audience", "aud",
		"--github-allowed-org", "org",
		"--allowed-image-prefix", "ghcr.io/test/",
	})
	if err == nil {
		t.Fatal("expected error for missing valkey addr")
	}
}

func TestParseWebConfig_FromEnv(t *testing.T) {
	t.Setenv("VALKEY_ADDR", "env-host:6379")
	t.Setenv("GITHUB_OIDC_AUDIENCE", "env-aud")
	t.Setenv("GITHUB_ALLOWED_ORG", "env-org")
	t.Setenv("ALLOWED_IMAGE_PREFIX", "ghcr.io/env/")
	t.Setenv("VALKEY_TLS_ENABLED", "true")
	t.Setenv("DEV_MODE", "true")

	cfg, err := ParseWebConfig([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ValkeyAddr != "env-host:6379" {
		t.Errorf("expected env-host:6379, got %s", cfg.ValkeyAddr)
	}
	if cfg.GithubOIDCAudience != "env-aud" {
		t.Errorf("expected env-aud, got %s", cfg.GithubOIDCAudience)
	}
	if !cfg.ValkeyTLS {
		t.Error("expected TLS to be enabled from env")
	}
	if !cfg.DevMode {
		t.Error("expected dev mode to be enabled from env")
	}
}

func TestParseWebConfig_FlagsOverrideEnv(t *testing.T) {
	t.Setenv("VALKEY_ADDR", "env-host:6379")
	t.Setenv("GITHUB_OIDC_AUDIENCE", "env-aud")
	t.Setenv("GITHUB_ALLOWED_ORG", "env-org")
	t.Setenv("ALLOWED_IMAGE_PREFIX", "ghcr.io/env/")

	cfg, err := ParseWebConfig([]string{
		"--valkey-addr", "flag-host:6379",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ValkeyAddr != "flag-host:6379" {
		t.Errorf("expected flag-host:6379, got %s", cfg.ValkeyAddr)
	}
}

func TestParseWorkerConfig_Defaults(t *testing.T) {
	cfg, err := ParseWorkerConfig([]string{
		"--valkey-addr", "localhost:6379",
		"--allowed-image-prefix", "ghcr.io/test/",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ValkeyChannel != "kuberollouttrigger" {
		t.Errorf("expected default channel, got %s", cfg.ValkeyChannel)
	}
	if cfg.Kubeconfig != "" {
		t.Errorf("expected empty kubeconfig, got %s", cfg.Kubeconfig)
	}
}

func TestParseWorkerConfig_MissingRequired(t *testing.T) {
	_, err := ParseWorkerConfig([]string{})
	if err == nil {
		t.Fatal("expected error for missing required config")
	}
}

func TestParseWorkerConfig_WithKubeconfig(t *testing.T) {
	cfg, err := ParseWorkerConfig([]string{
		"--valkey-addr", "localhost:6379",
		"--allowed-image-prefix", "ghcr.io/test/",
		"--kubeconfig", "/home/user/.kube/config",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Kubeconfig != "/home/user/.kube/config" {
		t.Errorf("expected kubeconfig path, got %s", cfg.Kubeconfig)
	}
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"debug", "DEBUG"},
		{"info", "INFO"},
		{"warn", "WARN"},
		{"error", "ERROR"},
		{"DEBUG", "DEBUG"},
		{"WARN", "WARN"},
		{"unknown", "INFO"},
		{"", "INFO"},
	}

	for _, tt := range tests {
		level := ParseLogLevel(tt.input)
		if level.String() != tt.expected {
			t.Errorf("ParseLogLevel(%q) = %s, want %s", tt.input, level.String(), tt.expected)
		}
	}
}

func TestEnvBool(t *testing.T) {
	tests := []struct {
		value    string
		expected bool
	}{
		{"true", true},
		{"TRUE", true},
		{"True", true},
		{"1", true},
		{"false", false},
		{"0", false},
		{"", false},
	}

	for _, tt := range tests {
		os.Setenv("TEST_BOOL", tt.value)
		if got := envBool("TEST_BOOL"); got != tt.expected {
			t.Errorf("envBool(%q) = %v, want %v", tt.value, got, tt.expected)
		}
	}
	os.Unsetenv("TEST_BOOL")
}

func TestNewRedisOptions(t *testing.T) {
	cfg := &CommonConfig{
		ValkeyAddr:     "localhost:6379",
		ValkeyUsername: "user",
		ValkeyPassword: "pass",
		ValkeyTLS:      true,
	}

	opts := cfg.NewRedisOptions()
	if opts.Addr != "localhost:6379" {
		t.Errorf("expected addr localhost:6379, got %s", opts.Addr)
	}
	if opts.Username != "user" {
		t.Errorf("expected username user, got %s", opts.Username)
	}
	if opts.Password != "pass" {
		t.Errorf("expected password pass, got %s", opts.Password)
	}
	if opts.TLSConfig == nil {
		t.Error("expected TLS config to be set")
	}
}

func TestNewRedisOptions_NoTLS(t *testing.T) {
	cfg := &CommonConfig{
		ValkeyAddr: "localhost:6379",
	}

	opts := cfg.NewRedisOptions()
	if opts.TLSConfig != nil {
		t.Error("expected no TLS config")
	}
}
