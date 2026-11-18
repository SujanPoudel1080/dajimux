package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/SujanPoudel1080/dajimux/internal/daemon"
	"github.com/SujanPoudel1080/dajimux/internal/protocol"
	"github.com/SujanPoudel1080/dajimux/internal/service"
)

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

func newDaemonCommand() *Command {
	return &Command{
		Name:        "daemon",
		Usage:       "dajimux daemon [--socket <path>]",
		Description: "Run the background session daemon",
		Run: func(args []string) error {
			runDaemonCmd(args)
			return nil
		},
	}
}

func newStopServerCommand() *Command {
	return &Command{
		Name:        "kill-server",
		Aliases:     []string{"stop"},
		Usage:       "dajimux stop / kill-server",
		Description: "Gracefully stop the background daemon",
		Run: func(args []string) error {
			runStopServerCmd()
			return nil
		},
	}
}

func newServiceCommand() *Command {
	return &Command{
		Name:        "service",
		Usage:       "dajimux service [install | status | uninstall]",
		Description: "Manage daemon background service",
		Run: func(args []string) error {
			runServiceCmd(args)
			return nil
		},
	}
}
