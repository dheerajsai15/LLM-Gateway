package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"time"

	"go.yaml.in/yaml/v3"
)

// defaultAnthropicURL is used when the config does not set anthropic_url.
const defaultAnthropicURL = "https://api.anthropic.com"

type Config struct {
	ListenAddr  string        `yaml:"listen_addr"`
	UpstreamURL string        `yaml:"upstream_url"`
	APIKey      string        `yaml:"-"`
	IdleTimeout time.Duration `yaml:"idle_timeout"`
	Models      []string      `yaml:"models"`

	AnthropicAPIKey string `yaml:"-"`
	AnthropicURL    string `yaml:"anthropic_url"`
}

func Load(path string) (Config, error) {
	cfg := Config{}
	data, err := os.ReadFile(path)
	if err != nil {
		err = fmt.Errorf("read config %s: %w", path, err)
		return cfg, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		err = fmt.Errorf("parse config %s: %w", path, err)
		return cfg, err
	}

	if envAddr := os.Getenv("GATEWAY_LISTEN_ADDR"); envAddr != "" {
		cfg.ListenAddr = envAddr
	}

	if envURL := os.Getenv("GATEWAY_UPSTREAM_URL"); envURL != "" {
		cfg.UpstreamURL = envURL
	}

	cfg.APIKey = os.Getenv("OPENAI_API_KEY")
	cfg.AnthropicAPIKey = os.Getenv("ANTHROPIC_API_KEY")

	envIdleTime := os.Getenv("GATEWAY_IDLE_TIMEOUT")

	if envIdleTime != "" {
		idleDuration, err := time.ParseDuration(envIdleTime)
		if err != nil {
			return cfg, fmt.Errorf("invalid value provided for GATEWAY_IDLE_TIMEOUT %q: %w", envIdleTime, err)
		}
		cfg.IdleTimeout = idleDuration
	}

	if err := cfg.validate(); err != nil {
		return cfg, fmt.Errorf("invalid config %s: %w", path, err)
	}
	return cfg, nil
}

func (cfg *Config) validate() error {
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":8080"
	}

	if cfg.APIKey == "" {
		return errors.New("OPENAI_API_KEY is not set")
	}

	if cfg.AnthropicAPIKey == "" {
		return errors.New("ANTHROPIC_API_KEY is not set")
	}

	if err := validateURL("upstream_url", cfg.UpstreamURL); err != nil {
		return err
	}

	if cfg.AnthropicURL == "" {
		cfg.AnthropicURL = defaultAnthropicURL
	}

	if err := validateURL("anthropic_url", cfg.AnthropicURL); err != nil {
		return err
	}

	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = 60 * time.Second
	} else if cfg.IdleTimeout < 0 {
		return fmt.Errorf("idle_timeout cannot be negative, provided value: %q", cfg.IdleTimeout)
	}

	return nil
}

// validateURL checks that raw is an http or https URL with a host. The name
// is the config setting, so the error tells the user which setting to fix.
func validateURL(name, raw string) error {
	if raw == "" {
		return fmt.Errorf("%s is not set", name)
	}

	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse %s: %w", name, err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%s %q: scheme is not http or https", name, raw)
	}

	if u.Host == "" {
		return fmt.Errorf("%s %q: host is empty", name, raw)
	}

	return nil
}
