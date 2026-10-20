package config

import (
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

	verr := cfg.validate()
	if verr != nil {
		return cfg, fmt.Errorf("invalid config %s: %w", path, verr)
	}
	return cfg, nil
}

func (cfg *Config) validate() error {
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":8080"
	}

	if cfg.UpstreamURL == "" {
		return fmt.Errorf("Upstream URL not set!")
	}

	u, err := url.Parse(cfg.UpstreamURL)
	if err != nil {
		return fmt.Errorf("URL parsing error: %w", err)
	}

	if u.Host == "" {
		return fmt.Errorf("Host is empty! upstream_url: %v", cfg.UpstreamURL)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("Schema is not http or https! upstream_url: %v", cfg.UpstreamURL)
	}

	return nil
}
