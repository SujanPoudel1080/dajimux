package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/SujanPoudel1080/dajimux/internal/client"
	"github.com/SujanPoudel1080/dajimux/internal/config"
	"github.com/SujanPoudel1080/dajimux/internal/daemon"
	"github.com/SujanPoudel1080/dajimux/internal/state"
)

func loadClientConfig() *config.Config {
	var cfgPath string
	for i := 1; i < len(os.Args); i++ {
		if (os.Args[i] == "--config" || os.Args[i] == "-c") && i+1 < len(os.Args) {
			cfgPath = os.Args[i+1]
			break
		}
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not load config %s: %v\n", cfgPath, err)
		return config.DefaultConfig()
	}
	return cfg
}

func runDefaultClient(sessionName string) {
	sockPath := daemon.DefaultSocketPath()

	// Ensure daemon is running; spawn in background if not
	if !isDaemonRunning(sockPath) {
		fmt.Println("Spawning dajimux daemon in background...")
		bin, err := os.Executable()
		if err != nil {
			bin = "dajimux"
		}
		bin = strings.TrimSuffix(bin, " (deleted)")
		daemonCmd := exec.Command(bin, "daemon")
		daemonCmd.SysProcAttr = daemonSysProcAttr()

		// Detach standard streams from controlling terminal to prevent SIGTTOU/SIGTTIN on Linux
		if devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0); err == nil {
			daemonCmd.Stdin = devNull
			daemonCmd.Stdout = devNull
			daemonCmd.Stderr = devNull
		}

		if err := daemonCmd.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to auto-spawn daemon: %v\n", err)
			os.Exit(1)
		}

		// Wait briefly for socket to become available (up to 3 seconds)
		running := false
		for i := 0; i < 60; i++ {
			time.Sleep(50 * time.Millisecond)
			if isDaemonRunning(sockPath) {
				running = true
				break
			}
		}
		if !running {
			fmt.Fprintf(os.Stderr, "Daemon failed to start within timeout. Check permissions at %s\n", sockPath)
			os.Exit(1)
		}
	}

	cfg := loadClientConfig()
	cli := client.NewClientWithConfig(sockPath, sessionName, cfg)
	if err := cli.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "dajimux error: %v\n", err)
		os.Exit(1)
	}
}

func runRestoreCmd(sessionName string) {
	st, err := state.LoadStateFromFile("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load session state: %v\n", err)
		os.Exit(1)
	}
	if len(st.Sessions) == 0 {
		fmt.Println("No persisted sessions found in", state.DefaultStatePath())
		return
	}
	if _, ok := st.Sessions[sessionName]; !ok {
		fmt.Printf("Session %q not found in persisted state. Available sessions:\n", sessionName)
		for name := range st.Sessions {
			fmt.Printf("  - %s\n", name)
		}
		return
	}

	fmt.Printf("Restoring session %q from %s...\n", sessionName, state.DefaultStatePath())
	runDefaultClient(sessionName)
}

func newAttachCommand() *Command {
	return &Command{
		Name:        "attach",
		Aliases:     []string{"a", "resume"},
		Usage:       "dajimux attach / a / resume [name]",
		Description: "Attach to or resume a session (-t <name>)",
		Run: func(args []string) error {
			sessionName := "main"
			for i := 0; i < len(args); i++ {
				if (args[i] == "-t" || args[i] == "--target") && i+1 < len(args) {
					sessionName = args[i+1]
					i++
				} else if !strings.HasPrefix(args[i], "-") {
					sessionName = args[i]
				}
			}
			runDefaultClient(sessionName)
			return nil
		},
	}
}

func newRestoreCommand() *Command {
	return &Command{
		Name:        "restore",
		Usage:       "dajimux restore [name]",
		Description: "Restore session from persisted state",
		Run: func(args []string) error {
			sessionName := "main"
			if len(args) > 0 {
				sessionName = args[0]
			}
			runRestoreCmd(sessionName)
			return nil
		},
	}
}
