package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config represents the full user configuration for dajimux.
type Config struct {
	General GeneralConfig `yaml:"general"`
	Input   InputConfig   `yaml:"input"`
	Theme   ThemeConfig   `yaml:"theme"`
}

// GeneralConfig holds general runtime parameters.
type GeneralConfig struct {
	DefaultShell    string `yaml:"default_shell"`
	ScrollbackLines int    `yaml:"scrollback_lines"`
	MouseSupport    bool   `yaml:"mouse_support"`
}

// InputConfig holds keybinding and modal behavior settings.
type InputConfig struct {
	PrefixMode       string `yaml:"prefix_mode"` // "modal" (Space+Space) or "prefix" (Ctrl+b)
	ModalDoubleTapMs int    `yaml:"modal_double_tap_ms"`
	CustomLeader     string `yaml:"custom_leader"`
}

// ThemeConfig holds ANSI colors and styling for borders, status bar, and palette.
type ThemeConfig struct {
	BorderActive      string `yaml:"border_active"`
	BorderInactive    string `yaml:"border_inactive"`
	StatusBarBg       string `yaml:"status_bar_bg"`
	StatusBarFg       string `yaml:"status_bar_fg"`
	PaletteBg         string `yaml:"palette_bg"`
	PaletteSelectedBg string `yaml:"palette_selected_bg"`
}

// DefaultConfig returns the sensible standard configuration for dajimux.
func DefaultConfig() *Config {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}

	return &Config{
		General: GeneralConfig{
			DefaultShell:    shell,
			ScrollbackLines: 5000,
			MouseSupport:    true,
		},
		Input: InputConfig{
			PrefixMode:       "modal",
			ModalDoubleTapMs: 280,
			CustomLeader:     " ",
		},
		Theme: ThemeConfig{
			BorderActive:      "\033[1m\033[38;5;51m", // Bright Cyan (Bold)
			BorderInactive:    "\033[38;5;240m",       // Dim Gray
			StatusBarBg:       "\033[48;5;236m",       // Dark Gray
			StatusBarFg:       "\033[38;5;250m",       // Light Gray
			PaletteBg:         "\033[48;5;235m",       // Deeper Gray
			PaletteSelectedBg: "\033[48;5;238m\033[1m",// Highlight Gray
		},
	}
}

// DefaultConfigPath returns ~/.config/dajimux/config.yaml or equivalent.
func DefaultConfigPath() string {
	if envPath := os.Getenv("DAJIMUX_CONFIG"); envPath != "" {
		return envPath
	}
	if xdgConfig := os.Getenv("XDG_CONFIG_HOME"); xdgConfig != "" {
		return filepath.Join(xdgConfig, "dajimux", "config.yaml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "config.yaml"
	}
	return filepath.Join(home, ".config", "dajimux", "config.yaml")
}

// Load reads and parses the YAML configuration from path.
// If path is empty, DefaultConfigPath() is used.
// If the configuration file does not exist, DefaultConfig() is returned without error.
func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultConfigPath()
	}

	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config file: %w", err)
	}

	// Intermediate unmarshal struct to allow string color names/escapes or raw YAML
	type rawThemeConfig struct {
		BorderActive      string `yaml:"border_active"`
		BorderInactive    string `yaml:"border_inactive"`
		StatusBarBg       string `yaml:"status_bar_bg"`
		StatusBarFg       string `yaml:"status_bar_fg"`
		PaletteBg         string `yaml:"palette_bg"`
		PaletteSelectedBg string `yaml:"palette_selected_bg"`
	}

	type rawConfig struct {
		General GeneralConfig  `yaml:"general"`
		Input   InputConfig    `yaml:"input"`
		Theme   rawThemeConfig `yaml:"theme"`
	}

	var raw rawConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse yaml config: %w", err)
	}

	if raw.General.DefaultShell != "" {
		cfg.General.DefaultShell = raw.General.DefaultShell
	}
	if raw.General.ScrollbackLines > 0 {
		cfg.General.ScrollbackLines = raw.General.ScrollbackLines
	}
	if raw.Input.PrefixMode != "" {
		cfg.Input.PrefixMode = raw.Input.PrefixMode
	}
	if raw.Input.ModalDoubleTapMs > 0 {
		cfg.Input.ModalDoubleTapMs = raw.Input.ModalDoubleTapMs
	}
	if raw.Input.CustomLeader != "" {
		cfg.Input.CustomLeader = raw.Input.CustomLeader
	}

	// Theme color mapping: supports ANSI escapes directly or standard color codes
	if raw.Theme.BorderActive != "" {
		cfg.Theme.BorderActive = parseColor(raw.Theme.BorderActive, cfg.Theme.BorderActive)
	}
	if raw.Theme.BorderInactive != "" {
		cfg.Theme.BorderInactive = parseColor(raw.Theme.BorderInactive, cfg.Theme.BorderInactive)
	}
	if raw.Theme.StatusBarBg != "" {
		cfg.Theme.StatusBarBg = parseBgColor(raw.Theme.StatusBarBg, cfg.Theme.StatusBarBg)
	}
	if raw.Theme.StatusBarFg != "" {
		cfg.Theme.StatusBarFg = parseColor(raw.Theme.StatusBarFg, cfg.Theme.StatusBarFg)
	}
	if raw.Theme.PaletteBg != "" {
		cfg.Theme.PaletteBg = parseBgColor(raw.Theme.PaletteBg, cfg.Theme.PaletteBg)
	}
	if raw.Theme.PaletteSelectedBg != "" {
		cfg.Theme.PaletteSelectedBg = parseBgColor(raw.Theme.PaletteSelectedBg, cfg.Theme.PaletteSelectedBg)
	}

	return cfg, nil
}

// parseColor converts color names (cyan, blue, red, etc.) or raw ANSI escape codes.
func parseColor(val, fallback string) string {
	if strings.HasPrefix(val, "\033[") {
		return val
	}
	switch strings.ToLower(strings.TrimSpace(val)) {
	case "cyan":
		return "\033[1m\033[38;5;51m"
	case "blue":
		return "\033[1m\033[38;5;39m"
	case "green":
		return "\033[1m\033[38;5;46m"
	case "yellow":
		return "\033[1m\033[38;5;226m"
	case "magenta", "purple":
		return "\033[1m\033[38;5;201m"
	case "red":
		return "\033[1m\033[38;5;196m"
	case "white":
		return "\033[1m\033[38;5;255m"
	case "gray", "grey":
		return "\033[38;5;242m"
	default:
		return fallback
	}
}

// parseBgColor converts background color names or raw ANSI escape codes.
func parseBgColor(val, fallback string) string {
	if strings.HasPrefix(val, "\033[") {
		return val
	}
	switch strings.ToLower(strings.TrimSpace(val)) {
	case "black":
		return "\033[48;5;232m"
	case "darkgray", "darkgrey":
		return "\033[48;5;235m"
	case "gray", "grey":
		return "\033[48;5;238m"
	case "blue":
		return "\033[48;5;24m"
	case "navy":
		return "\033[48;5;17m"
	default:
		return fallback
	}
}

// GenerateDefaultConfigFile writes a fully commented default configuration to path.
// If path is empty, DefaultConfigPath() is used.
func GenerateDefaultConfigFile(path string) (string, error) {
	if path == "" {
		path = DefaultConfigPath()
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create config directory: %w", err)
	}

	template := `# dajimux configuration file
# Placed at ~/.config/dajimux/config.yaml

general:
  # Default shell to spawn in new panes ($SHELL if empty)
  default_shell: ""
  # Maximum lines of scrollback history per pane
  scrollback_lines: 5000
  # Enable terminal mouse support (click to focus, drag to resize, wheel to scroll)
  mouse_support: true

input:
  # Mode switching behavior: "modal" (Vim Space+Space)
  prefix_mode: "modal"
  # Milliseconds threshold for double-tap space
  modal_double_tap_ms: 280
  # Custom leader key
  custom_leader: " "

theme:
  # Border color for active pane: cyan, blue, green, yellow, red, magenta, white
  border_active: "cyan"
  # Border color for inactive panes: gray, darkgray, white
  border_inactive: "gray"
  # Status bar background: darkgray, black, gray, navy, blue
  status_bar_bg: "darkgray"
  # Status bar text color: white, gray, yellow, cyan
  status_bar_fg: "white"
  # Command palette background: darkgray, black
  palette_bg: "darkgray"
  # Command palette selected item background: gray, blue, navy
  palette_selected_bg: "gray"
`

	if err := os.WriteFile(path, []byte(template), 0644); err != nil {
		return "", fmt.Errorf("write config template: %w", err)
	}

	return path, nil
}
