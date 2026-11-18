package main

import (
	"fmt"
	"os"

	"github.com/SujanPoudel1080/dajimux/internal/completion"
	"github.com/SujanPoudel1080/dajimux/internal/config"
	"github.com/SujanPoudel1080/dajimux/internal/man"
)

func runConfigCmd(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: dajimux config [path | init | show]")
		return
	}

	switch args[0] {
	case "path":
		fmt.Println(config.DefaultConfigPath())
	case "init":
		path, err := config.GenerateDefaultConfigFile("")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating config file: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Default configuration created at: %s\n", path)
	case "show":
		cfg := loadClientConfig()
		fmt.Printf("Config (from %s):\n", config.DefaultConfigPath())
		fmt.Printf("  Prefix Mode:        %s\n", cfg.Input.PrefixMode)
		fmt.Printf("  Modal Double-Tap:   %d ms\n", cfg.Input.ModalDoubleTapMs)
		fmt.Printf("  Scrollback Lines:   %d\n", cfg.General.ScrollbackLines)
		fmt.Printf("  Default Shell:      %s\n", cfg.General.DefaultShell)
	default:
		fmt.Fprintf(os.Stderr, "Unknown config action: %s\n", args[0])
		os.Exit(1)
	}
}

func runCompletionCmd(args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: dajimux completion <bash|zsh|fish>\n")
		os.Exit(1)
	}
	script, err := completion.Generate(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(script)
}

func runManCmd(args []string) {
	raw := false
	if len(args) > 0 && (args[0] == "--raw" || args[0] == "-r") {
		raw = true
	}
	if err := man.View(version, raw, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "Error displaying man page: %v\n", err)
		os.Exit(1)
	}
}

func newConfigCommand() *Command {
	return &Command{
		Name:        "config",
		Usage:       "dajimux config [init|path|show]",
		Description: "Manage user configuration file",
		Run: func(args []string) error {
			runConfigCmd(args)
			return nil
		},
	}
}

func newCompletionCommand() *Command {
	return &Command{
		Name:        "completion",
		Usage:       "dajimux completion [shell]",
		Description: "Generate shell completion (bash, zsh, fish)",
		Run: func(args []string) error {
			runCompletionCmd(args)
			return nil
		},
	}
}

func newManCommand() *Command {
	return &Command{
		Name:        "man",
		Usage:       "dajimux man [--raw]",
		Description: "Display manual page",
		Run: func(args []string) error {
			runManCmd(args)
			return nil
		},
	}
}

func newVersionCommand() *Command {
	return &Command{
		Name:        "version",
		Aliases:     []string{"--version", "-v"},
		Usage:       "dajimux version",
		Description: "Print version information",
		Run: func(args []string) error {
			fmt.Printf("dajimux v%s (commit: %s, built: %s)\n", version, commit, date)
			return nil
		},
	}
}

func newHelpCommand() *Command {
	return &Command{
		Name:        "help",
		Aliases:     []string{"--help", "-h"},
		Usage:       "dajimux help",
		Description: "Show help and quick reference",
		Run: func(args []string) error {
			printHelp()
			return nil
		},
	}
}
