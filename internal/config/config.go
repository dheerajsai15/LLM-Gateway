package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"

	"go.yaml.in/yaml/v3"
)

type Config struct {
	ListenAddr  string `yaml:"listen_addr"`
	UpstreamURL string `yaml:"upstream_url"`
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

	if err := cfg.validate(); err != nil {
		return cfg, fmt.Errorf("invalid config %s: %w", path, err)
	}
	return cfg, nil
}

func (cfg *Config) validate() error {
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":8080"
	}

	if cfg.UpstreamURL == "" {
		return errors.New("upstream URL not set!")
	}

	u, err := url.Parse(cfg.UpstreamURL)
	if err != nil {
		return fmt.Errorf("url parsing error: %w", err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme is not http or https! upstream_url: %q", cfg.UpstreamURL)
	}

	if u.Host == "" {
		return fmt.Errorf("host is empty! upstream_url: %q", cfg.UpstreamURL)
	}

	return nil
}
