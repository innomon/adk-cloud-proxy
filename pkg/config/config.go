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
	OpenAI OpenAIConfig `yaml:"openai"`
	Auth   AuthConfig   `yaml:"auth"`
}

// AuthConfig holds the configuration for pluggable auth validators.
type AuthConfig struct {
	Type   string                 `yaml:"type"`
	Config map[string]interface{} `yaml:"config"`
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

// OpenAIConfig holds OpenAI-compatible proxy settings.
type OpenAIConfig struct {
	ApiKey        string     `yaml:"api_key"`
	Auth          AuthConfig `yaml:"auth"`
	DefaultAppID  string     `yaml:"default_app_id"`
	DefaultUserID string     `yaml:"default_user_id"`
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
