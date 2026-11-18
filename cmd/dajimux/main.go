package main

import (
	"fmt"
	"os"
)

var (
	version = "0.2.3"
	commit  = "dev"
	date    = "unknown"
)

func main() {
	if len(os.Args) < 2 {
		runDefaultClient("main")
		return
	}

	reg := BuildDefaultRegistry()
	cmdName := os.Args[1]

	if cmd, ok := reg.Find(cmdName); ok {
		if err := cmd.Run(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// If argument does not start with '-', treat it as session name
	if cmdName[0] != '-' {
		runDefaultClient(cmdName)
		return
	}

	printHelp()
}

func printHelp() {
	helpText := `dajimux - Modern terminal multiplexer & agent mesh

Usage:
  dajimux [session-name]       Attach to or create a session
  dajimux list / ls            List active sessions and pane counts
  dajimux daemon               Run the background session daemon
  dajimux stop / kill-server   Gracefully stop the background daemon
  dajimux kill-session <name>  Terminate a specific active session
  dajimux rename-session <old> <new>  Rename an active session
  dajimux session [rename|kill|ls]    Manage sessions
  dajimux attach / a / resume [name]  Attach to or resume a session (-t <name>)
  dajimux restore [name]       Restore session from persisted state
  dajimux service [action]     Manage daemon service (install, status, uninstall)
  dajimux mcp [flags]          Run Model Context Protocol (MCP) server over stdio
  dajimux config [init|path]   Manage user configuration file
  dajimux completion [shell]   Generate shell completion (bash, zsh, fish)
  dajimux man [--raw]          Display manual page
  dajimux agent status [flags] Report agent state & telemetry badge
  dajimux msg send [flags]     Send message to another pane or channel
  dajimux pane capture [flags] Capture lines from a sibling pane
  dajimux peer listen [flags]  Listen for incoming peer connections
  dajimux peer connect <addr>  Connect to a remote dajimux peer session
  dajimux layout [preset]      Switch or cycle layouts (cycle, bsp, master, grid)
  dajimux workspace [action]   Manage workspaces (rename <name>, switch <n>)
  dajimux version              Print version information

Vim Modal Controls:
  Space + Space                Switch from INSERT mode to NORMAL mode
  In NORMAL Mode:
    h, j, k, l                 Spatial pane navigation
    w                          Swap active pane with next pane
    H, J, K, L                 Spatial pane swap (left, down, up, right)
    t                          Cycle layout preset (BSP -> Master-Stack -> Grid)
    s                          Split pane horizontally
    v                          Split pane vertically
    m <1-9>                    Move active pane to workspace 1..9
    x                          Close focused pane
    z                          Toggle monocle (fullscreen zoom)
    d                          Detach session (daemon keeps running)
    [                          Enter scrollback vi-mode
    : / Space                  Open command palette
    ?                          Toggle quick reference cheat sheet
    r                          Enter resize mode
    1 - 9                      Switch workspace
    i, a, Enter                Return to INSERT mode
`
	fmt.Print(helpText)
}
