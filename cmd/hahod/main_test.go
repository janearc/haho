package main

import (
	"testing"
)

func TestRequireLoopback(t *testing.T) {
	cases := []struct {
		addr string
		ok   bool
	}{
		{"127.0.0.1:8790", true},
		{"[::1]:8790", true},
		{"localhost:8790", true},
		{"0.0.0.0:8790", false},
		{"192.168.1.5:8790", false},
		{"example.com:8790", false},
		{":8790", false},     // empty host is not loopback
		{"8790", false},      // no host:port split
		{"127.0.0.1", false}, // no port
	}
	for _, c := range cases {
		err := requireLoopback(c.addr)
		if c.ok && err != nil {
			t.Errorf("requireLoopback(%q) = %v, want nil", c.addr, err)
		}
		if !c.ok && err == nil {
			t.Errorf("requireLoopback(%q) = nil, want refusal", c.addr)
		}
	}
}

func TestEnvOr(t *testing.T) {
	t.Setenv("HAHO_TEST_ENVOR", "from-env")
	if got := envOr("HAHO_TEST_ENVOR", "def"); got != "from-env" {
		t.Fatalf("set var: %q", got)
	}
	if got := envOr("HAHO_TEST_ENVOR_UNSET", "def"); got != "def" {
		t.Fatalf("unset var: %q", got)
	}
}

// TestConfigPrecedence pins flag over environment over default.
func TestConfigPrecedence(t *testing.T) {
	t.Setenv("HAHOD_ADDR", "127.0.0.1:9999")
	t.Setenv("HAHOD_STORE", "/env/store.db")

	// Flag beats env.
	cfg, err := loadConfig([]string{"-addr", "127.0.0.1:1111"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.addr != "127.0.0.1:1111" {
		t.Fatalf("flag should beat env: %q", cfg.addr)
	}
	// Env beats default where no flag is passed.
	if cfg.storePath != "/env/store.db" {
		t.Fatalf("env should beat default: %q", cfg.storePath)
	}
}

func TestConfigDefaults(t *testing.T) {
	// The harness may set these; force the default path.
	t.Setenv("HAHOD_ADDR", "")
	t.Setenv("HAHOD_STORE", "")
	cfg, err := loadConfig(nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.addr != "127.0.0.1:8790" {
		t.Fatalf("default addr: %q", cfg.addr)
	}
	if cfg.storePath == "" {
		t.Fatal("default store path empty")
	}
}
