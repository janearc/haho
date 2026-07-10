package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config is the top-level configuration for the haho harness.
type Config struct {
	ListenAddr string        `json:"listen_addr"`
	Model      ModelConfig   `json:"model"`
	SurrealDB  SurrealConfig `json:"surrealdb"`
	Filters    FiltersConfig `json:"filters"`
	Cache      CacheConfig   `json:"cache"`
}

// ModelConfig describes the LLM provider connection.
type ModelConfig struct {
	Provider  string            `json:"provider"`
	Endpoint  string            `json:"endpoint"`
	ModelName string            `json:"model_name"`
	APIKeyEnv string            `json:"api_key_env,omitempty"`
	Params    map[string]string `json:"params,omitempty"`
}

// SurrealConfig describes the SurrealDB connection.
type SurrealConfig struct {
	Discovery      string `json:"discovery"`
	Namespace      string `json:"namespace"`
	ServiceName    string `json:"service_name"`
	Database       string `json:"database"`
	CredentialsEnv string `json:"credentials_env,omitempty"`
	Host           string `json:"host,omitempty"`
	Port           int    `json:"port,omitempty"`
}

// FiltersConfig holds input and output filter pipelines.
type FiltersConfig struct {
	Input  []FilterEntry `json:"input"`
	Output []FilterEntry `json:"output"`
}

// FilterEntry is a single filter in a pipeline.
type FilterEntry struct {
	Name   string            `json:"name"`
	Type   string            `json:"type"`
	Params map[string]string `json:"params,omitempty"`
}

// CacheConfig controls response caching.
type CacheConfig struct {
	Enabled    bool `json:"enabled"`
	TTLSeconds int  `json:"ttl_seconds"`
}

// Load reads a JSON config file from path and returns a validated Config.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return &cfg, nil
}

// validate checks that required fields are present and sane.
func (c *Config) validate() error {
	if c.ListenAddr == "" {
		return fmt.Errorf("listen_addr must not be empty")
	}

	switch c.Model.Provider {
	case "local", "opus", "deepseek":
		// valid
	case "":
		return fmt.Errorf("model.provider must not be empty")
	default:
		return fmt.Errorf("model.provider %q is not supported (want local, opus, or deepseek)", c.Model.Provider)
	}

	switch c.SurrealDB.Discovery {
	case "static":
		if c.SurrealDB.Host == "" || c.SurrealDB.Port <= 0 {
			return fmt.Errorf("surrealdb.discovery=static requires host and a positive port")
		}
	case "k3s":
		if c.SurrealDB.ServiceName == "" || c.SurrealDB.Namespace == "" {
			return fmt.Errorf("surrealdb.discovery=k3s requires service_name and namespace")
		}
	case "":
		return fmt.Errorf("surrealdb.discovery must be set (static or k3s)")
	default:
		return fmt.Errorf("surrealdb.discovery %q is not supported (want static or k3s)", c.SurrealDB.Discovery)
	}

	if c.Cache.Enabled && c.Cache.TTLSeconds <= 0 {
		return fmt.Errorf("cache.ttl_seconds must be a positive number of seconds when cache is enabled")
	}

	return nil
}

// Default returns a Config with sensible defaults matching configs/default.json.
func Default() *Config {
	return &Config{
		ListenAddr: ":9090",
		Model: ModelConfig{
			Provider:  "local",
			Endpoint:  "http://localhost:11434",
			ModelName: "llama3",
		},
		SurrealDB: SurrealConfig{
			// The default deployment connects to an exposed SurrealDB port
			// (one harness container per operator instance, all pointed at the
			// same SurrealDB). k3s discovery stays available for in-cluster runs.
			Discovery:      "static",
			Namespace:      "haho",
			ServiceName:    "surrealdb",
			Database:       "haho",
			Host:           "localhost",
			Port:           8000,
			CredentialsEnv: "SURREAL_CREDENTIALS",
		},
		Filters: FiltersConfig{
			Input: []FilterEntry{
				{Name: "rtk", Type: "RTK", Params: map[string]string{}},
			},
			Output: []FilterEntry{
				{Name: "passthrough", Type: "PASSTHROUGH", Params: map[string]string{}},
			},
		},
		Cache: CacheConfig{
			Enabled:    true,
			TTLSeconds: 3600,
		},
	}
}
