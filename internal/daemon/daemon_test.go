package daemon

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SujanPoudel1080/dajimux/internal/protocol"
)

func TestDaemonLifecycleAndSession(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dajimux-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	sockPath := filepath.Join(tmpDir, "test-daemon.sock")
	d := NewDaemon(sockPath)
	if err := d.Start(); err != nil {
		t.Fatalf("failed to start daemon: %v", err)
	}
	defer d.Stop()

	go func() {
		_ = d.AcceptLoop()
	}()

	// Connect a client via Unix socket
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to connect to daemon socket: %v", err)
	}
	defer conn.Close()

	// Send Attach
	attachPayload, _ := json.Marshal(protocol.AttachPayload{
		SessionName: "test-session",
		Cols:        80,
		Rows:        24,
	})
	err = protocol.WriteEnvelope(conn, protocol.Envelope{
		Type:    protocol.MsgAttach,
		Payload: attachPayload,
	})
	if err != nil {
		t.Fatalf("failed to write attach envelope: %v", err)
	}

	// Read initial LayoutState
	reader := protocol.NewEnvelopeReader(conn)
	env, err := reader.Read()
	if err != nil {
		t.Fatalf("failed to read response envelope: %v", err)
	}
	if env.Type != protocol.MsgLayoutState {
		t.Fatalf("expected layout_state, got %s", env.Type)
	}

	var state protocol.LayoutStatePayload
	if err := json.Unmarshal(env.Payload, &state); err != nil {
		t.Fatalf("failed to unmarshal layout state: %v", err)
	}
	if state.SessionName != "test-session" {
		t.Errorf("expected session name test-session, got %s", state.SessionName)
	}
	if len(state.Panes) != 1 {
		t.Errorf("expected 1 initial pane, got %d", len(state.Panes))
	}

	// Test Agent Status update
	agentStatusPayload, _ := json.Marshal(protocol.AgentStatusPayload{
		State: "coding",
		Badge: "🤖 Generating tests",
	})
	err = protocol.WriteEnvelope(conn, protocol.Envelope{
		Type:    protocol.MsgAgentStatus,
		PaneID:  state.ActivePaneID,
		Payload: agentStatusPayload,
	})
	if err != nil {
		t.Fatalf("failed to send agent status: %v", err)
	}

	// Allow broker callback to update
	time.Sleep(50 * time.Millisecond)

	sess := d.Sessions["test-session"]
	if sess == nil {
		t.Fatalf("session not found in daemon")
	}
	pane := sess.Panes[state.ActivePaneID]
	if pane == nil || pane.AgentBadge != "🤖 Generating tests" {
		t.Errorf("expected agent badge to be updated")
	}

	// Test ListSessions request
	err = protocol.WriteEnvelope(conn, protocol.Envelope{
		Type: protocol.MsgListSessions,
	})
	if err != nil {
		t.Fatalf("failed to write list sessions envelope: %v", err)
	}

	var listEnv *protocol.Envelope
	for i := 0; i < 10; i++ {
		e, err := reader.Read()
		if err != nil {
			t.Fatalf("failed to read envelope: %v", err)
		}
		if e.Type == protocol.MsgListSessionsRes {
			listEnv = e
			break
		}
	}
	if listEnv == nil {
		t.Fatalf("expected list_sessions_res envelope, none received")
	}

	var listRes protocol.ListSessionsResponse
	if err := json.Unmarshal(listEnv.Payload, &listRes); err != nil {
		t.Fatalf("failed to unmarshal list response: %v", err)
	}
	if len(listRes.Sessions) == 0 {
		t.Fatalf("expected at least 1 session in list response")
	}
	found := false
	for _, s := range listRes.Sessions {
		if s.Name == "test-session" {
			found = true
			if s.WorkspacesCount != 1 || s.PanesCount != 1 {
				t.Errorf("unexpected counts: %+v", s)
			}
		}
	}
	if !found {
		t.Errorf("test-session not found in list response")
	}
}
