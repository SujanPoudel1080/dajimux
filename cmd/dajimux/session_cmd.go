package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/SujanPoudel1080/dajimux/internal/daemon"
	"github.com/SujanPoudel1080/dajimux/internal/protocol"
)

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

func runRenameSessionCmd(args []string) {
	if len(args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: dajimux rename-session <old-name> <new-name>\n")
		os.Exit(1)
	}
	oldName := args[0]
	newName := args[1]

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

	reqData, _ := json.Marshal(protocol.RenameSessionRequest{
		OldName: oldName,
		NewName: newName,
	})
	if err := protocol.WriteEnvelope(conn, protocol.Envelope{
		Type:    protocol.MsgRenameSession,
		Payload: reqData,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Rename session request failed: %v\n", err)
		os.Exit(1)
	}

	reader := protocol.NewEnvelopeReader(conn)
	for i := 0; i < 10; i++ {
		env, err := reader.Read()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Read failed: %v\n", err)
			os.Exit(1)
		}
		if env.Type == protocol.MsgRenameSessionRes {
			var res protocol.RenameSessionResponse
			_ = json.Unmarshal(env.Payload, &res)
			if !res.Success {
				fmt.Fprintf(os.Stderr, "Failed to rename session: %s\n", res.Error)
				os.Exit(1)
			}
			fmt.Printf("Session %q renamed to %q successfully.\n", oldName, newName)
			return
		}
	}
	fmt.Fprintf(os.Stderr, "Timeout waiting for daemon response.\n")
	os.Exit(1)
}

func runSessionCmd(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: dajimux session [rename <old> <new> | kill <name> | list]")
		return
	}
	sub := args[0]
	switch sub {
	case "rename":
		runRenameSessionCmd(args[1:])
	case "kill":
		runKillSessionCmd(args[1:])
	case "list", "ls":
		runListCmd()
	default:
		fmt.Fprintf(os.Stderr, "Unknown session action: %s. Use: rename, kill, list\n", sub)
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
		action = protocol.ActionPayload{Action: protocol.ActionCycleLayout}
	case "bsp":
		action = protocol.ActionPayload{Action: protocol.ActionSetLayout, Params: map[string]string{"type": "bsp"}}
	case "master", "master-stack":
		action = protocol.ActionPayload{Action: protocol.ActionSetLayout, Params: map[string]string{"type": "master_stack"}}
	case "grid":
		action = protocol.ActionPayload{Action: protocol.ActionSetLayout, Params: map[string]string{"type": "grid"}}
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
			Action: protocol.ActionRenameWorkspace,
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
			Action: protocol.ActionSwitchWorkspace,
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

func newListCommand() *Command {
	return &Command{
		Name:        "list",
		Aliases:     []string{"ls"},
		Usage:       "dajimux list / ls",
		Description: "List active sessions and pane counts",
		Run: func(args []string) error {
			runListCmd()
			return nil
		},
	}
}

func newKillSessionCommand() *Command {
	return &Command{
		Name:        "kill-session",
		Usage:       "dajimux kill-session <name>",
		Description: "Terminate a specific active session",
		Run: func(args []string) error {
			runKillSessionCmd(args)
			return nil
		},
	}
}

func newRenameSessionCommand() *Command {
	return &Command{
		Name:        "rename-session",
		Usage:       "dajimux rename-session <old> <new>",
		Description: "Rename an active session",
		Run: func(args []string) error {
			runRenameSessionCmd(args)
			return nil
		},
	}
}

func newSessionCommand() *Command {
	return &Command{
		Name:        "session",
		Usage:       "dajimux session [rename|kill|ls]",
		Description: "Manage sessions",
		Run: func(args []string) error {
			runSessionCmd(args)
			return nil
		},
	}
}

func newLayoutCommand() *Command {
	return &Command{
		Name:        "layout",
		Usage:       "dajimux layout [preset]",
		Description: "Switch or cycle layouts (cycle, bsp, master, grid)",
		Run: func(args []string) error {
			runLayoutCmd(args)
			return nil
		},
	}
}

func newWorkspaceCommand() *Command {
	return &Command{
		Name:        "workspace",
		Aliases:     []string{"ws"},
		Usage:       "dajimux workspace [action]",
		Description: "Manage workspaces (rename <name>, switch <n>)",
		Run: func(args []string) error {
			runWorkspaceCmd(args)
			return nil
		},
	}
}
