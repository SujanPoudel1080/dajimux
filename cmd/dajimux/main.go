package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"time"

	"github.com/SujanPoudel1080/dajimux/internal/client"
	"github.com/SujanPoudel1080/dajimux/internal/daemon"
	"github.com/SujanPoudel1080/dajimux/internal/peer"
	"github.com/SujanPoudel1080/dajimux/internal/protocol"
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

	case "peer":
		runPeerCmd(os.Args[2:])

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
  dajimux attach <name>        Attach to a named session
  dajimux agent status [flags] Report agent state & telemetry badge
  dajimux msg send [flags]     Send message to another pane or channel
  dajimux pane capture [flags] Capture lines from a sibling pane
  dajimux peer listen [flags]  Listen for incoming peer connections
  dajimux peer connect <addr>  Connect to a remote dajimux peer session
  dajimux version              Print version information

Vim Modal Controls:
  Space + Space                Switch from INSERT mode to NORMAL mode
  In NORMAL Mode:
    h, j, k, l                 Spatial pane navigation
    s                          Split pane horizontally
    v                          Split pane vertically
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

	cli := client.NewClient(sockPath, sessionName)
	if err := cli.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "dajimux error: %v\n", err)
		os.Exit(1)
	}
}

func runDaemonCmd(args []string) {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	sockPath := fs.String("socket", daemon.DefaultSocketPath(), "Path to unix domain socket")
	_ = fs.Parse(args)

	d := daemon.NewDaemon(*sockPath)
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
