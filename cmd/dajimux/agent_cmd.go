package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/SujanPoudel1080/dajimux/internal/client"
	"github.com/SujanPoudel1080/dajimux/internal/daemon"
	"github.com/SujanPoudel1080/dajimux/internal/mcp"
	"github.com/SujanPoudel1080/dajimux/internal/peer"
	"github.com/SujanPoudel1080/dajimux/internal/protocol"
)

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

func newAgentCommand() *Command {
	return &Command{
		Name:        "agent",
		Usage:       "dajimux agent status [flags]",
		Description: "Report agent state & telemetry badge",
		Run: func(args []string) error {
			runAgentCmd(args)
			return nil
		},
	}
}

func newMsgCommand() *Command {
	return &Command{
		Name:        "msg",
		Usage:       "dajimux msg send [flags]",
		Description: "Send message to another pane or channel",
		Run: func(args []string) error {
			runMsgCmd(args)
			return nil
		},
	}
}

func newPaneCommand() *Command {
	return &Command{
		Name:        "pane",
		Usage:       "dajimux pane capture [flags]",
		Description: "Capture lines from a sibling pane",
		Run: func(args []string) error {
			runPaneCmd(args)
			return nil
		},
	}
}

func newMCPCommand() *Command {
	return &Command{
		Name:        "mcp",
		Usage:       "dajimux mcp [flags]",
		Description: "Run Model Context Protocol (MCP) server over stdio",
		Run: func(args []string) error {
			runMCPCmd(args)
			return nil
		},
	}
}

func newPeerCommand() *Command {
	return &Command{
		Name:        "peer",
		Usage:       "dajimux peer [listen|connect]",
		Description: "Peer-to-peer remote session collaboration",
		Run: func(args []string) error {
			runPeerCmd(args)
			return nil
		},
	}
}
