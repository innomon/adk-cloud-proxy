package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config represents the overall configuration.
type Config struct {
	PubSub PubSubConfig `yaml:"pubsub"`
	Proxy  ProxyConfig  `yaml:"proxy"`
}

// PubSubConfig holds pubsub configuration.
type PubSubConfig struct {
	Type   string                 `yaml:"type"`
	Config map[string]interface{} `yaml:"config"`
}

// ProxyConfig holds proxy-related configuration.
type ProxyConfig struct {
	URL string `yaml:"url"` // The URL the connector should connect to (e.g., Cloud Run URL)
}

// LoadConfig loads the configuration from a file.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %v", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %v", err)
	}

	return &cfg, nil
}
