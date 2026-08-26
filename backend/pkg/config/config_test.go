package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadParsesDurations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("http:\n  host: 127.0.0.1\n  port: 8080\n  read_header_timeout: 2s\n  write_timeout: 3s\nwebsocket:\n  ping_interval: 1s\n  max_message_bytes: 1024\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	read, write, err := c.HTTPTimeouts()
	if err != nil || read != 2*time.Second || write != 3*time.Second {
		t.Fatalf("timeouts: %v %v %v", read, write, err)
	}
	if c.MaxMessageBytes() != 1024 {
		t.Fatalf("message limit: %d", c.MaxMessageBytes())
	}
}

func TestLoadAppliesEnvironmentOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("http:\n  host: 0.0.0.0\n  port: 8080\nrooms:\n  max_roles: 10\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PLANNING_POKER_HTTP_PORT", "9090")
	t.Setenv("PLANNING_POKER_ROOMS_MAX_ROLES", "4")
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.HTTP.Port != 9090 || c.Rooms.MaxRoles != 4 {
		t.Fatalf("unexpected overrides: %#v", c)
	}
}
