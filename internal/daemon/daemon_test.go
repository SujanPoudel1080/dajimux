package daemon

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SujanPoudel1080/dajimux/internal/bsp"
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

func TestSessionSwapAndMove(t *testing.T) {
	s, err := NewSession("swap-test", 100, 40, nil)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Split horizontally to get a second pane
	p2, err := s.SplitActive(bsp.SplitHorizontal)
	if err != nil || p2 == nil {
		t.Fatalf("failed to split: %v", err)
	}

	ws := s.Workspaces[1]
	p2ID := p2.ID

	// Active should be p2
	if ws.ActivePaneID() != p2ID {
		t.Errorf("expected active pane %s, got %s", p2ID, ws.ActivePaneID())
	}

	// Swap with next
	if err := s.SwapActiveWithNext(); err != nil {
		t.Fatalf("SwapActiveWithNext failed: %v", err)
	}

	// Move active pane to Workspace 2
	if err := s.MoveActiveToWorkspace(2); err != nil {
		t.Fatalf("MoveActiveToWorkspace failed: %v", err)
	}

	if s.ActiveWS != 2 {
		t.Errorf("expected active workspace 2, got %d", s.ActiveWS)
	}

	ws2 := s.Workspaces[2]
	if ws2 == nil || ws2.ActivePaneID() != p2ID {
		t.Errorf("expected workspace 2 to contain pane %s", p2ID)
	}

	// Workspace 1 should still exist and have a valid active pane
	if ws1 := s.Workspaces[1]; ws1 == nil || ws1.ActivePaneID() == "" {
		t.Errorf("expected workspace 1 to still have a valid pane")
	}

	// FocusPane test
	s.SwitchWorkspace(1)
	ws1 := s.Workspaces[1]
	currentActive := ws1.ActivePaneID()
	leaves := ws1.Tree.ListLeafPanes()
	if len(leaves) > 0 {
		s.FocusPane(leaves[0])
		if ws1.ActivePaneID() != leaves[0] {
			t.Errorf("expected focus on %s, got %s", leaves[0], ws1.ActivePaneID())
		}
	}
	_ = currentActive
}

func TestDaemonSaveAndRestoreState(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dajimux-persist-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	statePath := filepath.Join(tmpDir, "session_state.json")
	sockPath := filepath.Join(tmpDir, "test.sock")

	d := NewDaemon(sockPath)
	sess, err := d.GetOrCreateSession("persist-test", 80, 24)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Split to create 2 panes
	_, err = sess.SplitActive(bsp.SplitVertical)
	if err != nil {
		t.Fatalf("failed to split: %v", err)
	}

	// Save daemon state
	if err := d.SaveState(statePath); err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	// Create new daemon instance and restore
	d2 := NewDaemon(filepath.Join(tmpDir, "test2.sock"))
	if err := d2.RestoreSessions(statePath); err != nil {
		t.Fatalf("RestoreSessions failed: %v", err)
	}

	restoredSess := d2.Sessions["persist-test"]
	if restoredSess == nil {
		t.Fatalf("expected restored session 'persist-test'")
	}
	if len(restoredSess.Panes) != 2 {
		t.Errorf("expected 2 panes in restored session, got %d", len(restoredSess.Panes))
	}
}

func TestDaemonListPanesAndSpawnPane(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dajimux-panes-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	sockPath := filepath.Join(tmpDir, "panes.sock")
	d := NewDaemon(sockPath)
	if err := d.Start(); err != nil {
		t.Fatalf("failed to start daemon: %v", err)
	}
	defer d.Stop()

	go func() {
		_ = d.AcceptLoop()
	}()

	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	reader := protocol.NewEnvelopeReader(conn)

	// Attach to session
	attachPayload, _ := json.Marshal(protocol.AttachPayload{
		SessionName: "test-mcp",
		Cols:        80,
		Rows:        24,
	})
	_ = protocol.WriteEnvelope(conn, protocol.Envelope{
		Type:    protocol.MsgAttach,
		Payload: attachPayload,
	})

	// Discard attach layout state
	_, _ = reader.Read()

	// 1. Request ListPanes for "test-mcp"
	reqData, _ := json.Marshal(protocol.ListPanesRequest{SessionName: "test-mcp"})
	_ = protocol.WriteEnvelope(conn, protocol.Envelope{
		Type:    protocol.MsgListPanes,
		Payload: reqData,
	})

	var listEnv *protocol.Envelope
	for i := 0; i < 10; i++ {
		e, err := reader.Read()
		if err != nil {
			t.Fatalf("failed to read: %v", err)
		}
		if e.Type == protocol.MsgListPanesRes {
			listEnv = e
			break
		}
	}
	if listEnv == nil {
		t.Fatalf("expected MsgListPanesRes envelope")
	}

	var listRes protocol.ListPanesResponse
	if err := json.Unmarshal(listEnv.Payload, &listRes); err != nil {
		t.Fatalf("failed to unmarshal list panes: %v", err)
	}
	if len(listRes.Panes) != 1 {
		t.Fatalf("expected 1 pane in test-mcp, got %d", len(listRes.Panes))
	}
	p1 := listRes.Panes[0]
	if p1.SessionName != "test-mcp" || p1.PaneID == "" {
		t.Errorf("unexpected pane info: %+v", p1)
	}

	// 2. Request SpawnPane
	spawnReq, _ := json.Marshal(protocol.SpawnPaneRequest{
		SessionName: "test-mcp",
		Direction:   "horizontal",
	})
	_ = protocol.WriteEnvelope(conn, protocol.Envelope{
		Type:    protocol.MsgSpawnPane,
		Payload: spawnReq,
	})

	var spawnEnv *protocol.Envelope
	for i := 0; i < 10; i++ {
		e, err := reader.Read()
		if err != nil {
			t.Fatalf("failed to read: %v", err)
		}
		if e.Type == protocol.MsgSpawnPaneRes {
			spawnEnv = e
			break
		}
	}
	if spawnEnv == nil {
		t.Fatalf("expected MsgSpawnPaneRes envelope")
	}

	var spawnRes protocol.SpawnPaneResponse
	if err := json.Unmarshal(spawnEnv.Payload, &spawnRes); err != nil {
		t.Fatalf("failed to unmarshal spawn res: %v", err)
	}
	if !spawnRes.Success || spawnRes.PaneID == "" {
		t.Errorf("expected spawn success, got %+v", spawnRes)
	}
}
