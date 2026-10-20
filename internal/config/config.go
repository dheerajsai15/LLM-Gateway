package config

import (
	"fmt"
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
	return cfg, nil
}
