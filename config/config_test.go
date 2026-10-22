package config

import (
	"os"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

func TestLoadConfigDefault(t *testing.T) {
	_ = os.Chdir("..")
	conf, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig(\"\") failed: %v", err)
	}

	if conf.Addr == "" {
		t.Errorf("expected non-empty Addr, got %q", conf.Addr)
	}
	if conf.Name != "controller01" {
		t.Errorf("expected Name 'controller01', got %q", conf.Name)
	}
	if conf.WSBroker != "https://localhost:8765" {
		t.Errorf("expected WSBroker 'https://localhost:8765', got %q", conf.WSBroker)
	}
	if len(conf.Targets) == 0 {
		t.Errorf("expected default targets, got none")
	}
	if conf.Timeout != 10*time.Second {
		t.Errorf("expected timeout 10s, got %v", conf.Timeout)
	}
	if conf.ReadLimit != 16*1024*1024 {
		t.Errorf("expected read_limit %d, got %d", 16*1024*1024, conf.ReadLimit)
	}
}

func TestLoadConfigCustomReadLimit(t *testing.T) {
	tomlData := `
addr = "localhost:8888"
name = "custom01"
read_limit = 33554432
`
	conf := &Config{
		ReadLimit: 16 * 1024 * 1024,
	}
	if _, err := toml.Decode(tomlData, conf); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if conf.ReadLimit != 33554432 {
		t.Errorf("expected read_limit 33554432, got %d", conf.ReadLimit)
	}
	if conf.Addr != "localhost:8888" {
		t.Errorf("expected addr localhost:8888, got %s", conf.Addr)
	}
}
