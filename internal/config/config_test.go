package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	data := []byte(`{
		"listen_addr": ":8080",
		"model": {
			"provider": "local",
			"endpoint": "http://localhost:11434",
			"model_name": "llama3"
		},
		"surrealdb": {
			"discovery": "k3s",
			"namespace": "default",
			"service_name": "surrealdb",
			"database": "haho"
		},
		"filters": {
			"input": [],
			"output": []
		},
		"cache": {
			"enabled": true,
			"ttl_seconds": 300
		}
	}`)

	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":8080")
	}
	if cfg.Model.Provider != "local" {
		t.Errorf("Model.Provider = %q, want %q", cfg.Model.Provider, "local")
	}
	if cfg.Model.ModelName != "llama3" {
		t.Errorf("Model.ModelName = %q, want %q", cfg.Model.ModelName, "llama3")
	}
	if cfg.SurrealDB.Database != "haho" {
		t.Errorf("SurrealDB.Database = %q, want %q", cfg.SurrealDB.Database, "haho")
	}
	if cfg.Cache.TTLSeconds != 300 {
		t.Errorf("Cache.TTLSeconds = %d, want %d", cfg.Cache.TTLSeconds, 300)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/config.json")
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")

	if err := os.WriteFile(path, []byte(`{not json`), 0644); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestLoad_ValidationError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty_addr.json")

	data := []byte(`{
		"listen_addr": "",
		"model": {
			"provider": "local",
			"endpoint": "http://localhost:11434",
			"model_name": "llama3"
		},
		"surrealdb": {
			"discovery": "k3s",
			"namespace": "default",
			"service_name": "surrealdb",
			"database": "haho"
		},
		"filters": { "input": [], "output": [] },
		"cache": { "enabled": false, "ttl_seconds": 0 }
	}`)

	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected validation error for empty listen_addr, got nil")
	}
}

func TestDefault(t *testing.T) {
	cfg := Default()
	if cfg == nil {
		t.Fatal("Default() returned nil")
	}
	if cfg.ListenAddr == "" {
		t.Error("Default ListenAddr is empty")
	}
	if cfg.Model.Provider == "" {
		t.Error("Default Model.Provider is empty")
	}
	if cfg.SurrealDB.Database == "" {
		t.Error("Default SurrealDB.Database is empty")
	}
	if !cfg.Cache.Enabled {
		t.Error("Default Cache.Enabled should be true")
	}
}

func TestValidate_MissingProvider(t *testing.T) {
	cfg := Default()
	cfg.Model.Provider = ""

	if err := cfg.validate(); err == nil {
		t.Fatal("expected error for empty provider, got nil")
	}
}

func TestValidate_InvalidProvider(t *testing.T) {
	cfg := Default()
	cfg.Model.Provider = "foo"

	if err := cfg.validate(); err == nil {
		t.Fatal("expected error for invalid provider 'foo', got nil")
	}
}
