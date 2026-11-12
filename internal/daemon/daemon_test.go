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

	d.mu.RLock()
	sess := d.Sessions["test-session"]
	d.mu.RUnlock()
	if sess == nil {
		t.Fatalf("session not found in daemon")
	}
	sess.mu.RLock()
	pane := sess.Panes[state.ActivePaneID]
	var badge string
	if pane != nil {
		pane.mu.RLock()
		badge = pane.AgentBadge
		pane.mu.RUnlock()
	}
	sess.mu.RUnlock()
	if pane == nil || badge != "🤖 Generating tests" {
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

func TestSessionLayoutCycleAndWorkspaceRename(t *testing.T) {
	sess, err := NewSession("test-layout-ws", 100, 30, nil)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Initial layout is BSP
	state := sess.GetLayoutState()
	if state.LayoutType != "bsp" {
		t.Errorf("expected layout bsp, got %s", state.LayoutType)
	}

	// Cycle layout -> master_stack
	sess.CycleLayout()
	state = sess.GetLayoutState()
	if state.LayoutType != "master_stack" {
		t.Errorf("expected layout master_stack, got %s", state.LayoutType)
	}

	// Cycle layout -> grid
	sess.CycleLayout()
	state = sess.GetLayoutState()
	if state.LayoutType != "grid" {
		t.Errorf("expected layout grid, got %s", state.LayoutType)
	}

	// Rename workspace 1
	sess.RenameWorkspace(1, "code")
	state = sess.GetLayoutState()
	found := false
	for _, ws := range state.Workspaces {
		if ws.ID == 1 && ws.Name == "code" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected workspace 1 to be named 'code', got %+v", state.Workspaces)
	}
}

func TestAutoPaneCleanupOnExit(t *testing.T) {
	sess, err := NewSession("test-auto-cleanup", 80, 24, nil)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	defer sess.Close()

	p2, err := sess.SplitActive(bsp.SplitVertical)
	if err != nil {
		t.Fatalf("failed to split: %v", err)
	}

	sess.mu.RLock()
	panesLen := len(sess.Panes)
	sess.mu.RUnlock()
	if panesLen != 2 {
		t.Fatalf("expected 2 panes, got %d", panesLen)
	}

	// Terminate p2's process to simulate shell exiting ('exit' or Ctrl+D)
	if p2.Cmd != nil && p2.Cmd.Process != nil {
		_ = p2.Cmd.Process.Kill()
	}

	// Wait up to 1 second for onExit callback to prune pane
	cleaned := false
	for i := 0; i < 20; i++ {
		time.Sleep(50 * time.Millisecond)
		sess.mu.RLock()
		_, exists := sess.Panes[p2.ID]
		sess.mu.RUnlock()
		if !exists {
			cleaned = true
			break
		}
	}

	if !cleaned {
		t.Fatalf("expected pane %s to be automatically cleaned up after process exit", p2.ID)
	}

	// Verify tree has only 1 pane left
	ws := sess.ActiveWorkspace()
	leaves := ws.Tree.ListLeafPanes()
	if len(leaves) != 1 {
		t.Fatalf("expected 1 pane left in tree, got %d", len(leaves))
	}
}

func TestDaemonKillSessionAndClosePane(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dajimux-kill-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	sockPath := filepath.Join(tmpDir, "kill.sock")
	d := NewDaemon(sockPath)
	if err := d.Start(); err != nil {
		t.Fatalf("failed to start daemon: %v", err)
	}
	defer d.Stop()

	go func() {
		_ = d.AcceptLoop()
	}()

	sess, err := d.GetOrCreateSession("kill-target", 80, 24)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	p2, err := sess.SplitActive(bsp.SplitVertical)
	if err != nil {
		t.Fatalf("failed to split: %v", err)
	}

	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	reader := protocol.NewEnvelopeReader(conn)

	// Test MsgClosePane
	closeReq, _ := json.Marshal(protocol.ClosePaneRequest{PaneID: p2.ID})
	_ = protocol.WriteEnvelope(conn, protocol.Envelope{
		Type:    protocol.MsgClosePane,
		Payload: closeReq,
	})

	for {
		env, err := reader.Read()
		if err != nil {
			t.Fatalf("read failed: %v", err)
		}
		if env.Type == protocol.MsgClosePaneRes {
			var res protocol.ClosePaneResponse
			_ = json.Unmarshal(env.Payload, &res)
			if !res.Success {
				t.Fatalf("expected close pane success, got error: %s", res.Error)
			}
			break
		}
	}

	sess.mu.RLock()
	_, p2StillExists := sess.Panes[p2.ID]
	sess.mu.RUnlock()
	if p2StillExists {
		t.Errorf("pane %s should have been removed after MsgClosePane", p2.ID)
	}

	// Test MsgKillSession
	killReq, _ := json.Marshal(protocol.KillSessionRequest{SessionName: "kill-target"})
	_ = protocol.WriteEnvelope(conn, protocol.Envelope{
		Type:    protocol.MsgKillSession,
		Payload: killReq,
	})

	for {
		env, err := reader.Read()
		if err != nil {
			t.Fatalf("read failed: %v", err)
		}
		if env.Type == protocol.MsgKillSessionRes {
			var res protocol.KillSessionResponse
			_ = json.Unmarshal(env.Payload, &res)
			if !res.Success {
				t.Fatalf("expected kill session success, got error: %s", res.Error)
			}
			break
		}
	}

	d.mu.RLock()
	_, sessStillExists := d.Sessions["kill-target"]
	d.mu.RUnlock()
	if sessStillExists {
		t.Errorf("session 'kill-target' should have been destroyed after MsgKillSession")
	}
}

func TestDaemonRenameSessionAndWorkspace(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dajimux-rename-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	sockPath := filepath.Join(tmpDir, "rename.sock")
	d := NewDaemon(sockPath)
	if err := d.Start(); err != nil {
		t.Fatalf("failed to start daemon: %v", err)
	}
	defer d.Stop()

	go func() {
		_ = d.AcceptLoop()
	}()

	_, err = d.GetOrCreateSession("orig-name", 80, 24)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	reader := protocol.NewEnvelopeReader(conn)

	// Test MsgRenameWorkspace
	wsReq, _ := json.Marshal(protocol.RenameWorkspaceRequest{
		SessionName: "orig-name",
		WorkspaceID: 1,
		NewName:     "frontend",
	})
	_ = protocol.WriteEnvelope(conn, protocol.Envelope{
		Type:    protocol.MsgRenameWorkspace,
		Payload: wsReq,
	})

	for {
		env, err := reader.Read()
		if err != nil {
			t.Fatalf("read failed: %v", err)
		}
		if env.Type == protocol.MsgRenameWorkspaceRes {
			var res protocol.RenameWorkspaceResponse
			_ = json.Unmarshal(env.Payload, &res)
			if !res.Success {
				t.Fatalf("expected rename workspace success, got error: %s", res.Error)
			}
			break
		}
	}

	// Verify workspace name
	d.mu.RLock()
	sess := d.Sessions["orig-name"]
	d.mu.RUnlock()
	sess.mu.RLock()
	wsName := sess.Workspaces[1].Name
	sess.mu.RUnlock()
	if wsName != "frontend" {
		t.Errorf("expected workspace name 'frontend', got %q", wsName)
	}

	// Test MsgRenameSession
	sessReq, _ := json.Marshal(protocol.RenameSessionRequest{
		OldName: "orig-name",
		NewName: "renamed-session",
	})
	_ = protocol.WriteEnvelope(conn, protocol.Envelope{
		Type:    protocol.MsgRenameSession,
		Payload: sessReq,
	})

	for {
		env, err := reader.Read()
		if err != nil {
			t.Fatalf("read failed: %v", err)
		}
		if env.Type == protocol.MsgRenameSessionRes {
			var res protocol.RenameSessionResponse
			_ = json.Unmarshal(env.Payload, &res)
			if !res.Success {
				t.Fatalf("expected rename session success, got error: %s", res.Error)
			}
			break
		}
	}

	d.mu.RLock()
	_, oldExists := d.Sessions["orig-name"]
	newSess, newExists := d.Sessions["renamed-session"]
	d.mu.RUnlock()

	if oldExists {
		t.Errorf("old session 'orig-name' should no longer exist")
	}
	if !newExists || newSess.Name != "renamed-session" {
		t.Errorf("expected session 'renamed-session' to exist")
	}
}

func TestDaemonStreamPaneOutputMultiPaneIsolation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dajimux-stream-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	t.Setenv("DAJIMUX_STATE_PATH", filepath.Join(tmpDir, "state.json"))

	sockPath := filepath.Join(tmpDir, "stream.sock")
	d := NewDaemon(sockPath)
	if err := d.Start(); err != nil {
		t.Fatalf("failed to start daemon: %v", err)
	}
	defer d.Stop()

	go func() {
		_ = d.AcceptLoop()
	}()

	sess, err := d.GetOrCreateSession("stream-isolation", 100, 30)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	p2, err := sess.SplitActive(bsp.SplitVertical)
	if err != nil {
		t.Fatalf("failed to split: %v", err)
	}

	visible := sess.VisiblePanes()
	if len(visible) != 2 {
		t.Fatalf("expected 2 visible panes, got %d", len(visible))
	}

	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to dial daemon: %v", err)
	}
	defer conn.Close()

	reader := protocol.NewEnvelopeReader(conn)
	attachPayload, _ := json.Marshal(protocol.AttachPayload{
		SessionName: "stream-isolation",
		Cols:        100,
		Rows:        30,
	})
	if err := protocol.WriteEnvelope(conn, protocol.Envelope{
		Type:    protocol.MsgAttach,
		Payload: attachPayload,
	}); err != nil {
		t.Fatalf("failed to attach: %v", err)
	}

	// Send explicit input to both panes to ensure both generate output immediately
	p1 := visible[0]
	_, _ = p1.Write([]byte("echo P1_READY\n"))
	_, _ = p2.Write([]byte("echo P2_READY\n"))

	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	// Verify that MsgFrameUpdate messages include accurate PaneID
	receivedPanes := make(map[string]bool)
	for len(receivedPanes) < 2 {
		env, err := reader.Read()
		if err != nil {
			t.Fatalf("timed out waiting for frames from both panes; received for: %+v, err: %v", receivedPanes, err)
		}
		if env.Type == protocol.MsgFrameUpdate {
			if env.PaneID == "" {
				t.Fatalf("expected non-empty PaneID on MsgFrameUpdate envelope")
			}
			var f protocol.FrameUpdatePayload
			if err := json.Unmarshal(env.Payload, &f); err == nil {
				if f.PaneID != env.PaneID {
					t.Fatalf("envelope PaneID %q != payload PaneID %q", env.PaneID, f.PaneID)
				}
			}
			receivedPanes[env.PaneID] = true
		}
	}

	if !receivedPanes[p2.ID] {
		t.Errorf("expected frame updates for p2 %s", p2.ID)
	}
}
