package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.General.ScrollbackLines != 5000 {
		t.Fatalf("expected default scrollback 5000, got %d", cfg.General.ScrollbackLines)
	}
	if cfg.Input.ModalDoubleTapMs != 280 {
		t.Fatalf("expected default double tap ms 280, got %d", cfg.Input.ModalDoubleTapMs)
	}
	if cfg.Input.PrefixMode != "modal" {
		t.Fatalf("expected prefix mode modal, got %s", cfg.Input.PrefixMode)
	}
}

func TestLoadNonExistentFileReturnsDefaults(t *testing.T) {
	cfg, err := Load("/path/to/non/existent/config.yaml")
	if err != nil {
		t.Fatalf("expected nil error on missing config, got %v", err)
	}
	if cfg == nil || cfg.General.ScrollbackLines != 5000 {
		t.Fatalf("expected default config returned, got %+v", cfg)
	}
}

func TestLoadCustomConfig(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	yamlContent := `
general:
  default_shell: "/bin/bash"
  scrollback_lines: 10000

input:
  prefix_mode: "modal"
  modal_double_tap_ms: 350
  custom_leader: " "

theme:
  border_active: "green"
  border_inactive: "gray"
  status_bar_bg: "black"
  status_bar_fg: "yellow"
`
	if err := os.WriteFile(cfgPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.General.DefaultShell != "/bin/bash" {
		t.Errorf("expected default shell /bin/bash, got %s", cfg.General.DefaultShell)
	}
	if cfg.General.ScrollbackLines != 10000 {
		t.Errorf("expected scrollback lines 10000, got %d", cfg.General.ScrollbackLines)
	}
	if cfg.Input.ModalDoubleTapMs != 350 {
		t.Errorf("expected modal double tap ms 350, got %d", cfg.Input.ModalDoubleTapMs)
	}
	if cfg.Theme.BorderActive != "\033[1m\033[38;5;46m" {
		t.Errorf("expected green border active ANSI, got %q", cfg.Theme.BorderActive)
	}
}
