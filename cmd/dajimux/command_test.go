package main

import (
	"testing"
)

func TestRegistryRegistrationAndLookup(t *testing.T) {
	r := NewRegistry()

	ran := false
	cmd := &Command{
		Name:        "test",
		Aliases:     []string{"t", "tst"},
		Usage:       "dajimux test",
		Description: "A test command",
		Run: func(args []string) error {
			ran = true
			return nil
		},
	}

	r.Register(cmd)

	// Lookup by primary name
	found, ok := r.Find("test")
	if !ok || found == nil {
		t.Fatalf("expected to find command by primary name")
	}
	if found.Name != "test" {
		t.Errorf("expected name 'test', got %q", found.Name)
	}

	// Lookup by aliases
	for _, alias := range []string{"t", "tst"} {
		foundAlias, ok := r.Find(alias)
		if !ok || foundAlias != cmd {
			t.Errorf("expected to find command by alias %q", alias)
		}
	}

	// Lookup non-existent
	if _, ok := r.Find("nonexistent"); ok {
		t.Errorf("expected not to find nonexistent command")
	}

	// Execute Run
	if err := found.Run(nil); err != nil {
		t.Fatalf("unexpected error running command: %v", err)
	}
	if !ran {
		t.Errorf("expected command run function to be executed")
	}
}

func TestRegistryNilAndEmptySafety(t *testing.T) {
	r := NewRegistry()
	r.Register(nil)
	r.Register(&Command{})

	if len(r.Commands()) != 0 {
		t.Errorf("expected 0 commands registered, got %d", len(r.Commands()))
	}
}

func TestBuildDefaultRegistry(t *testing.T) {
	reg := BuildDefaultRegistry()

	expectedCommands := []string{
		"attach", "a", "resume",
		"list", "ls",
		"daemon",
		"kill-server", "stop",
		"kill-session",
		"rename-session",
		"session",
		"restore",
		"service",
		"mcp",
		"config",
		"completion",
		"man",
		"agent",
		"msg",
		"pane",
		"peer",
		"layout",
		"workspace", "ws",
		"version", "--version", "-v",
		"help", "--help", "-h",
	}

	for _, name := range expectedCommands {
		cmd, ok := reg.Find(name)
		if !ok || cmd == nil {
			t.Errorf("expected command or alias %q to be registered", name)
		}
	}

	// Test that version command executes without error
	verCmd, ok := reg.Find("version")
	if !ok {
		t.Fatalf("expected 'version' command to exist")
	}
	if err := verCmd.Run(nil); err != nil {
		t.Errorf("unexpected error running version command: %v", err)
	}
}
