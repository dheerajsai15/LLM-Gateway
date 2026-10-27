package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfig writes content to a config file in a temporary directory and
// returns its path. The directory is removed when the test ends.
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadAnthropic(t *testing.T) {
	const base = "upstream_url: \"http://localhost:9090\"\n"

	tests := []struct {
		name    string
		yaml    string
		key     string
		wantURL string
		wantErr string
	}{
		{
			name:    "default URL",
			yaml:    base,
			key:     "test-anthropic-key",
			wantURL: "https://api.anthropic.com",
		},
		{
			name:    "URL from the config file",
			yaml:    base + "anthropic_url: \"http://localhost:9091\"\n",
			key:     "test-anthropic-key",
			wantURL: "http://localhost:9091",
		},
		{
			name:    "URL without a scheme",
			yaml:    base + "anthropic_url: \"api.anthropic.com\"\n",
			key:     "test-anthropic-key",
			wantErr: `anthropic_url "api.anthropic.com": scheme is not http or https`,
		},
		{
			name:    "missing key",
			yaml:    base,
			key:     "",
			wantErr: "ANTHROPIC_API_KEY is not set",
		},
		{
			name:    "key in the config file is ignored",
			yaml:    base + "anthropicapikey: \"from-file\"\n",
			key:     "",
			wantErr: "ANTHROPIC_API_KEY is not set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// t.Setenv restores each variable when the test ends. An empty
			// value counts as not set, so the shell environment of the
			// developer cannot change the result.
			t.Setenv("OPENAI_API_KEY", "test-openai-key")
			t.Setenv("ANTHROPIC_API_KEY", tt.key)
			t.Setenv("GATEWAY_LISTEN_ADDR", "")
			t.Setenv("GATEWAY_UPSTREAM_URL", "")
			t.Setenv("GATEWAY_IDLE_TIMEOUT", "")

			cfg, err := Load(writeConfig(t, tt.yaml))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Load() error = %v, want an error that contains %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.AnthropicURL != tt.wantURL {
				t.Errorf("AnthropicURL = %q, want %q", cfg.AnthropicURL, tt.wantURL)
			}
			if cfg.AnthropicAPIKey != tt.key {
				t.Errorf("AnthropicAPIKey = %q, want %q", cfg.AnthropicAPIKey, tt.key)
			}
		})
	}
}
