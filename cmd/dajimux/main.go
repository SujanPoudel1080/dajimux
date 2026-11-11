package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"time"

	"github.com/SujanPoudel1080/dajimux/internal/client"
	"github.com/SujanPoudel1080/dajimux/internal/completion"
	"github.com/SujanPoudel1080/dajimux/internal/config"
	"github.com/SujanPoudel1080/dajimux/internal/daemon"
	"github.com/SujanPoudel1080/dajimux/internal/man"
	"github.com/SujanPoudel1080/dajimux/internal/mcp"
	"github.com/SujanPoudel1080/dajimux/internal/peer"
	"github.com/SujanPoudel1080/dajimux/internal/protocol"
	"github.com/SujanPoudel1080/dajimux/internal/service"
	"github.com/SujanPoudel1080/dajimux/internal/state"
)

var (
	version = "0.1.1"
	commit  = "dev"
	date    = "unknown"
)

func main() {
	if len(os.Args) < 2 {
		runDefaultClient("main")
		return
	}

	cmd := os.Args[1]

	switch cmd {
	case "version", "--version", "-v":
		fmt.Printf("dajimux v%s (commit: %s, built: %s)\n", version, commit, date)

	case "daemon":
		runDaemonCmd(os.Args[2:])

	case "attach":
		sessionName := "main"
		if len(os.Args) > 2 {
			sessionName = os.Args[2]
		}
		runDefaultClient(sessionName)

	case "agent":
		runAgentCmd(os.Args[2:])

	case "msg":
		runMsgCmd(os.Args[2:])

	case "pane":
		runPaneCmd(os.Args[2:])

	case "list", "ls":
		runListCmd()

	case "kill-server", "stop":
		runStopServerCmd()

	case "kill-session":
		runKillSessionCmd(os.Args[2:])

	case "peer":
		runPeerCmd(os.Args[2:])

	case "config":
		runConfigCmd(os.Args[2:])

	case "restore":
		sessionName := "main"
		if len(os.Args) > 2 {
			sessionName = os.Args[2]
		}
		runRestoreCmd(sessionName)

	case "service":
		runServiceCmd(os.Args[2:])

	case "mcp":
		runMCPCmd(os.Args[2:])

	case "completion":
		runCompletionCmd(os.Args[2:])

	case "man":
		runManCmd(os.Args[2:])

	case "layout":
		runLayoutCmd(os.Args[2:])

	case "workspace", "ws":
		runWorkspaceCmd(os.Args[2:])

	case "help", "--help", "-h":
		printHelp()

	default:
		// If argument does not start with '-', treat it as session name
		if cmd[0] != '-' {
			runDefaultClient(cmd)
		} else {
			printHelp()
		}
	}
}

func printHelp() {
	helpText := `dajimux - Modern terminal multiplexer & agent mesh

Usage:
  dajimux [session-name]       Attach to or create a session
  dajimux list / ls            List active sessions and pane counts
  dajimux daemon               Run the background session daemon
  dajimux stop / kill-server   Gracefully stop the background daemon
  dajimux kill-session <name>  Terminate a specific active session
  dajimux attach <name>        Attach to a named session
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
    r                          Enter resize mode
    1 - 9                      Switch workspace
    i, a, Enter                Return to INSERT mode
`
	fmt.Print(helpText)
}

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

func runServiceCmd(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: dajimux service [install | status | uninstall]")
		return
	}

	mgr, err := service.NewServiceManager()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Service manager error: %v\n", err)
		os.Exit(1)
	}

	switch args[0] {
	case "install":
		path, err := mgr.Install()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to install service: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("dajimux daemon service installed successfully:\n  %s\n", path)
	case "status":
		status, err := mgr.Status()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Service status error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(status)
	case "uninstall":
		if err := mgr.Uninstall(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to uninstall service: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("dajimux daemon service uninstalled.")
	default:
		fmt.Fprintf(os.Stderr, "Unknown service command %q. Usage: dajimux service [install | status | uninstall]\n", args[0])
		os.Exit(1)
	}
}

func runMCPCmd(args []string) {
	fs := flag.NewFlagSet("mcp", flag.ExitOnError)
	sockPath := fs.String("socket", daemon.DefaultSocketPath(), "Path to daemon unix domain socket")
	_ = fs.Parse(args)

	bridge := mcp.NewSocketDaemonBridge(*sockPath)
	srv := mcp.NewServer(bridge, version)
	if err := srv.Run(os.Stdin, os.Stdout); err != nil && err != io.EOF {
		fmt.Fprintf(os.Stderr, "MCP server error: %v\n", err)
		os.Exit(1)
	}
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
		daemonCmd := exec.Command(bin, "daemon")
		daemonCmd.SysProcAttr = daemonSysProcAttr()
		if err := daemonCmd.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to auto-spawn daemon: %v\n", err)
			os.Exit(1)
		}

		// Wait briefly for socket to become available
		for i := 0; i < 20; i++ {
			time.Sleep(50 * time.Millisecond)
			if isDaemonRunning(sockPath) {
				break
			}
		}
	}

	cfg := loadClientConfig()
	cli := client.NewClientWithConfig(sockPath, sessionName, cfg)
	if err := cli.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "dajimux error: %v\n", err)
		os.Exit(1)
	}
}

func runDaemonCmd(args []string) {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	sockPath := fs.String("socket", daemon.DefaultSocketPath(), "Path to unix domain socket")
	_ = fs.Parse(args)

	cfg := loadClientConfig()
	d := daemon.NewDaemonWithConfig(*sockPath, cfg)
	if err := d.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start daemon: %v\n", err)
		os.Exit(1)
	}
	defer d.Stop()

	fmt.Printf("dajimux daemon running on %s\n", *sockPath)
	if err := d.AcceptLoop(); err != nil {
		fmt.Fprintf(os.Stderr, "Daemon listener closed: %v\n", err)
	}
}

func runAgentCmd(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: dajimux agent status [--state <state>] [--badge <badge>] [--progress <0.0-1.0>]")
		return
	}

	sub := args[0]
	if sub == "status" {
		fs := flag.NewFlagSet("agent status", flag.ExitOnError)
		state := fs.String("state", "active", "Agent state (e.g. idle, thinking, executing)")
		badge := fs.String("badge", "", "Status badge description")
		progress := fs.Float64("progress", 0.0, "Progress proportion (0.0 - 1.0)")
		paneID := fs.String("pane", os.Getenv("DAJIMUX_PANE_ID"), "Target pane ID")
		_ = fs.Parse(args[1:])

		sockPath := os.Getenv("DAJIMUX_SOCK")
		if sockPath == "" {
			sockPath = daemon.DefaultSocketPath()
		}

		payload := protocol.AgentStatusPayload{
			State:    *state,
			Badge:    *badge,
			Progress: float32(*progress),
		}
		data, _ := json.Marshal(payload)

		env := protocol.Envelope{
			Type:    protocol.MsgAgentStatus,
			PaneID:  *paneID,
			Payload: data,
		}

		if err := sendEnvelopeOnce(sockPath, env); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to update agent status: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Agent status updated: [%s] %s\n", *state, *badge)
	}
}

func runMsgCmd(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: dajimux msg send [--target <target>] <message-content>")
		return
	}

	sub := args[0]
	if sub == "send" {
		fs := flag.NewFlagSet("msg send", flag.ExitOnError)
		target := fs.String("target", "@all", "Target pane ID or #channel")
		fromPane := fs.String("from", os.Getenv("DAJIMUX_PANE_ID"), "Sender pane ID")
		_ = fs.Parse(args[1:])

		content := fs.Arg(0)
		if content == "" {
			fmt.Println("Error: message content is required")
			os.Exit(1)
		}

		sockPath := os.Getenv("DAJIMUX_SOCK")
		if sockPath == "" {
			sockPath = daemon.DefaultSocketPath()
		}

		payload := protocol.AgentMessagePayload{
			FromPaneID: *fromPane,
			ToTarget:   *target,
			Content:    content,
			Timestamp:  time.Now().Unix(),
		}
		data, _ := json.Marshal(payload)

		env := protocol.Envelope{
			Type:    protocol.MsgAgentSend,
			Payload: data,
		}

		if err := sendEnvelopeOnce(sockPath, env); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to send message: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Message dispatched through agent mesh.")
	}
}

func runPaneCmd(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: dajimux pane capture [--pane <pane-id>] [--lines <n>]")
		return
	}

	sub := args[0]
	if sub == "capture" {
		fs := flag.NewFlagSet("pane capture", flag.ExitOnError)
		paneID := fs.String("pane", "", "Target pane ID to capture")
		lines := fs.Int("lines", 50, "Number of lines to capture")
		_ = fs.Parse(args[1:])

		if *paneID == "" {
			fmt.Println("Error: --pane is required")
			os.Exit(1)
		}

		sockPath := os.Getenv("DAJIMUX_SOCK")
		if sockPath == "" {
			sockPath = daemon.DefaultSocketPath()
		}

		conn, err := net.Dial("unix", sockPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Connect failed: %v\n", err)
			os.Exit(1)
		}
		defer conn.Close()

		req := protocol.PaneCaptureRequest{
			TargetPaneID: *paneID,
			Lines:        *lines,
		}
		data, _ := json.Marshal(req)
		_ = protocol.WriteEnvelope(conn, protocol.Envelope{
			Type:    protocol.MsgPaneCapture,
			Payload: data,
		})

		reader := protocol.NewEnvelopeReader(conn)
		res, err := reader.Read()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Read failed: %v\n", err)
			os.Exit(1)
		}

		var captureRes protocol.PaneCaptureResponse
		if err := json.Unmarshal(res.Payload, &captureRes); err == nil {
			for _, line := range captureRes.Lines {
				fmt.Println(line)
			}
		}
	}
}

func isDaemonRunning(sockPath string) bool {
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func sendEnvelopeOnce(sockPath string, env protocol.Envelope) error {
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		return err
	}
	defer conn.Close()
	return protocol.WriteEnvelope(conn, env)
}

func runPeerCmd(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: dajimux peer <listen|connect> [flags]")
		return
	}

	sub := args[0]
	switch sub {
	case "listen":
		fs := flag.NewFlagSet("peer listen", flag.ExitOnError)
		addr := fs.String("addr", ":7474", "Peering TCP address to listen on")
		token := fs.String("token", os.Getenv("DAJIMUX_PEER_TOKEN"), "Secret authentication token")
		nodeName := fs.String("name", "", "Node identifier")
		sockPath := fs.String("socket", daemon.DefaultSocketPath(), "Path to local daemon socket")
		_ = fs.Parse(args[1:])

		if *nodeName == "" {
			hostname, _ := os.Hostname()
			if hostname == "" {
				hostname = "dajimux-host"
			}
			*nodeName = hostname
		}

		if !isDaemonRunning(*sockPath) {
			fmt.Printf("Notice: Local daemon at %s is not currently responding. Incoming peers will connect once daemon is active.\n", *sockPath)
		}

		server := peer.NewServer(*addr, *token, *nodeName, func(peerNode string, conn net.Conn) {
			_ = peer.TunnelToUnix(conn, *sockPath)
		})

		if err := server.Listen(); err != nil {
			fmt.Fprintf(os.Stderr, "Peer listen failed: %v\n", err)
			os.Exit(1)
		}
		defer server.Close()

		fmt.Printf("dajimux peer listening on %s (node: %s)\n", server.ListenerAddr(), *nodeName)
		if err := server.Serve(); err != nil {
			fmt.Fprintf(os.Stderr, "Peer server error: %v\n", err)
		}

	case "connect":
		if len(args) < 2 {
			fmt.Println("Usage: dajimux peer connect <address> [--token <secret>] [--name <name>] [session-name]")
			return
		}
		addr := args[1]
		fs := flag.NewFlagSet("peer connect", flag.ExitOnError)
		token := fs.String("token", os.Getenv("DAJIMUX_PEER_TOKEN"), "Secret authentication token")
		nodeName := fs.String("name", "", "Local node identifier")
		_ = fs.Parse(args[2:])

		sessionName := "main"
		if fs.NArg() > 0 {
			sessionName = fs.Arg(0)
		}

		if *nodeName == "" {
			hostname, _ := os.Hostname()
			if hostname == "" {
				hostname = "dajimux-client"
			}
			*nodeName = hostname
		}

		fmt.Printf("Connecting to dajimux peer at %s...\n", addr)
		c := peer.NewClient(addr, *token, *nodeName)
		conn, hs, err := c.Connect()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Peer connection failed: %v\n", err)
			os.Exit(1)
		}
		defer conn.Close()

		fmt.Printf("Authenticated with remote node %q (v%s). Attaching to session %q...\n", hs.NodeName, hs.Version, sessionName)
		time.Sleep(200 * time.Millisecond)

		cli := client.NewClient("", sessionName)
		if err := cli.RunWithConn(conn); err != nil {
			fmt.Fprintf(os.Stderr, "dajimux remote session error: %v\n", err)
			os.Exit(1)
		}

	default:
		fmt.Printf("Unknown peer subcommand: %s (expected 'listen' or 'connect')\n", sub)
		os.Exit(1)
	}
}

func runListCmd() {
	sockPath := os.Getenv("DAJIMUX_SOCK")
	if sockPath == "" {
		sockPath = daemon.DefaultSocketPath()
	}

	if !isDaemonRunning(sockPath) {
		fmt.Println("No dajimux daemon is running.")
		return
	}

	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Connect failed: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	if err := protocol.WriteEnvelope(conn, protocol.Envelope{
		Type: protocol.MsgListSessions,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Request failed: %v\n", err)
		os.Exit(1)
	}

	reader := protocol.NewEnvelopeReader(conn)
	res, err := reader.Read()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Response failed: %v\n", err)
		os.Exit(1)
	}

	var listRes protocol.ListSessionsResponse
	if err := json.Unmarshal(res.Payload, &listRes); err != nil {
		fmt.Fprintf(os.Stderr, "Invalid response: %v\n", err)
		os.Exit(1)
	}

	if len(listRes.Sessions) == 0 {
		fmt.Println("No active sessions.")
		return
	}

	for _, s := range listRes.Sessions {
		attached := ""
		icon := "○"
		if s.AttachedClients > 0 {
			attached = " (attached)"
			icon = "●"
		}
		wsPlural := "workspace"
		if s.WorkspacesCount != 1 {
			wsPlural = "workspaces"
		}
		panePlural := "pane"
		if s.PanesCount != 1 {
			panePlural = "panes"
		}
		fmt.Printf("%s %s: %d %s, %d %s%s\n", icon, s.Name, s.WorkspacesCount, wsPlural, s.PanesCount, panePlural, attached)
	}
}

func runStopServerCmd() {
	sockPath := os.Getenv("DAJIMUX_SOCK")
	if sockPath == "" {
		sockPath = daemon.DefaultSocketPath()
	}

	if !isDaemonRunning(sockPath) {
		fmt.Println("No dajimux daemon is running.")
		return
	}

	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Connect failed: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	if err := protocol.WriteEnvelope(conn, protocol.Envelope{
		Type: protocol.MsgStopServer,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Stop request failed: %v\n", err)
		os.Exit(1)
	}

	reader := protocol.NewEnvelopeReader(conn)
	_, _ = reader.Read()

	fmt.Println("dajimux daemon stopped.")
}

func runKillSessionCmd(args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: dajimux kill-session <session-name>\n")
		os.Exit(1)
	}
	sessionName := args[0]

	sockPath := os.Getenv("DAJIMUX_SOCK")
	if sockPath == "" {
		sockPath = daemon.DefaultSocketPath()
	}

	if !isDaemonRunning(sockPath) {
		fmt.Println("No dajimux daemon is running.")
		return
	}

	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Connect failed: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	reqData, _ := json.Marshal(protocol.KillSessionRequest{SessionName: sessionName})
	if err := protocol.WriteEnvelope(conn, protocol.Envelope{
		Type:    protocol.MsgKillSession,
		Payload: reqData,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Kill session request failed: %v\n", err)
		os.Exit(1)
	}

	reader := protocol.NewEnvelopeReader(conn)
	for i := 0; i < 10; i++ {
		env, err := reader.Read()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Read failed: %v\n", err)
			os.Exit(1)
		}
		if env.Type == protocol.MsgKillSessionRes {
			var res protocol.KillSessionResponse
			_ = json.Unmarshal(env.Payload, &res)
			if !res.Success {
				fmt.Fprintf(os.Stderr, "Failed to kill session %q: %s\n", sessionName, res.Error)
				os.Exit(1)
			}
			fmt.Printf("Session %q terminated successfully.\n", sessionName)
			return
		}
	}
	fmt.Fprintf(os.Stderr, "Timeout waiting for daemon response.\n")
	os.Exit(1)
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

func runLayoutCmd(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: dajimux layout [cycle | bsp | master | grid]")
		return
	}
	sub := args[0]
	sockPath := daemon.DefaultSocketPath()

	var action protocol.ActionPayload
	switch sub {
	case "cycle":
		action = protocol.ActionPayload{Action: "cycle_layout"}
	case "bsp":
		action = protocol.ActionPayload{Action: "set_layout", Params: map[string]string{"type": "bsp"}}
	case "master", "master-stack":
		action = protocol.ActionPayload{Action: "set_layout", Params: map[string]string{"type": "master_stack"}}
	case "grid":
		action = protocol.ActionPayload{Action: "set_layout", Params: map[string]string{"type": "grid"}}
	default:
		fmt.Fprintf(os.Stderr, "Unknown layout: %s. Use: cycle, bsp, master, grid\n", sub)
		os.Exit(1)
	}

	data, _ := json.Marshal(action)
	env := protocol.Envelope{
		Type:    protocol.MsgAction,
		Payload: data,
	}
	if err := sendEnvelopeOnce(sockPath, env); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to send layout action: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Layout updated: %s\n", sub)
}

func runWorkspaceCmd(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: dajimux workspace [rename <name> [--workspace <n>] | switch <n>]")
		return
	}
	sub := args[0]
	sockPath := daemon.DefaultSocketPath()

	switch sub {
	case "rename":
		if len(args) < 2 {
			fmt.Println("Error: workspace name required. Usage: dajimux workspace rename <name> [--workspace <n>]")
			os.Exit(1)
		}
		name := args[1]
		ws := ""
		if len(args) >= 4 && (args[2] == "--workspace" || args[2] == "-w") {
			ws = args[3]
		}
		params := map[string]string{"name": name}
		if ws != "" {
			params["workspace"] = ws
		}
		data, _ := json.Marshal(protocol.ActionPayload{
			Action: "rename_workspace",
			Params: params,
		})
		if err := sendEnvelopeOnce(sockPath, protocol.Envelope{Type: protocol.MsgAction, Payload: data}); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to rename workspace: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Workspace renamed to %q\n", name)

	case "switch":
		if len(args) < 2 {
			fmt.Println("Usage: dajimux workspace switch <1-9>")
			os.Exit(1)
		}
		data, _ := json.Marshal(protocol.ActionPayload{
			Action: "switch_workspace",
			Params: map[string]string{"workspace": args[1]},
		})
		if err := sendEnvelopeOnce(sockPath, protocol.Envelope{Type: protocol.MsgAction, Payload: data}); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to switch workspace: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Switched to workspace %s\n", args[1])
	default:
		fmt.Fprintf(os.Stderr, "Unknown workspace command: %s. Use: rename, switch\n", sub)
		os.Exit(1)
	}
}
