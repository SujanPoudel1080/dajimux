package mcp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/SujanPoudel1080/dajimux/internal/protocol"
)

type mockDaemonBridge struct {
	panes         []protocol.DetailedPaneInfo
	capturedLines []string
	lastSentCmd   string
	lastPaneID    string
	closedPaneID  string
	lastState     string
	lastBadge     string
	spawnedPaneID string
}

func (m *mockDaemonBridge) ListPanes(sessionName string) ([]protocol.DetailedPaneInfo, error) {
	return m.panes, nil
}

func (m *mockDaemonBridge) ReadPane(paneID string, lines int) ([]string, error) {
	m.lastPaneID = paneID
	return m.capturedLines, nil
}

func (m *mockDaemonBridge) SendCommand(paneID, command string, pressEnter bool) error {
	m.lastPaneID = paneID
	m.lastSentCmd = command
	if pressEnter {
		m.lastSentCmd += "\n"
	}
	return nil
}

func (m *mockDaemonBridge) SpawnPane(session, direction, cwd, command string) (string, error) {
	return m.spawnedPaneID, nil
}

func (m *mockDaemonBridge) ClosePane(paneID string) error {
	m.closedPaneID = paneID
	return nil
}

func (m *mockDaemonBridge) SetAgentStatus(paneID, state, badge string) error {
	m.lastPaneID = paneID
	m.lastState = state
	m.lastBadge = badge
	return nil
}

func TestMCPServerLifecycleAndTools(t *testing.T) {
	bridge := &mockDaemonBridge{
		panes: []protocol.DetailedPaneInfo{
			{
				SessionName: "main",
				WorkspaceID: 1,
				PaneID:      "pane-1",
				Title:       "shell",
				Cwd:         "/Users/sujan/repo",
				IsActive:    true,
				AgentState:  "coding",
				AgentBadge:  "🤖 Testing",
			},
		},
		capturedLines: []string{"line 1", "line 2", "Done."},
		spawnedPaneID: "pane-2",
	}

	server := NewServer(bridge, "0.1.4")

	requests := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"list_panes","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"read_pane","arguments":{"pane_id":"pane-1","lines":10}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"send_command","arguments":{"pane_id":"pane-1","command":"echo hello"}}}`,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"spawn_pane","arguments":{"session":"main","direction":"vertical"}}}`,
		`{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"close_pane","arguments":{"pane_id":"pane-2"}}}`,
		`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"set_agent_status","arguments":{"pane_id":"pane-1","state":"building","badge":"🔨 Compiling"}}}`,
	}

	inBuf := bytes.NewBufferString(strings.Join(requests, "\n") + "\n")
	outBuf := &bytes.Buffer{}

	if err := server.Run(inBuf, outBuf); err != nil {
		t.Fatalf("server.Run error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(outBuf.String()), "\n")
	// Note: notifications/initialized produces no response, so 9 responses expected
	if len(lines) != 9 {
		t.Fatalf("expected 9 responses, got %d:\n%s", len(lines), outBuf.String())
	}

	// 1. Check initialize
	var resp1 JSONRPCResponse
	_ = json.Unmarshal([]byte(lines[0]), &resp1)
	if resp1.Error != nil || resp1.Result == nil {
		t.Errorf("initialize response error: %+v", resp1)
	}

	// 2. Check ping
	var resp2 JSONRPCResponse
	_ = json.Unmarshal([]byte(lines[1]), &resp2)
	if resp2.Error != nil {
		t.Errorf("ping error: %+v", resp2)
	}

	// 3. Check tools/list
	var resp3 struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Result  struct {
			Tools []Tool `json:"tools"`
		} `json:"result"`
	}
	_ = json.Unmarshal([]byte(lines[2]), &resp3)
	if len(resp3.Result.Tools) != 6 {
		t.Errorf("expected 6 tools in tools/list, got %d", len(resp3.Result.Tools))
	}

	// 4. Check list_panes tool call
	if !strings.Contains(lines[3], "pane-1") || !strings.Contains(lines[3], "/Users/sujan/repo") {
		t.Errorf("expected pane-1 in list_panes result: %s", lines[3])
	}

	// 5. Check read_pane tool call
	if !strings.Contains(lines[4], "Done.") || bridge.lastPaneID != "pane-1" {
		t.Errorf("expected Done. in read_pane result: %s", lines[4])
	}

	// 6. Check send_command tool call
	if bridge.lastSentCmd != "echo hello\n" {
		t.Errorf("expected 'echo hello\\n', got %q", bridge.lastSentCmd)
	}

	// 7. Check spawn_pane tool call
	if !strings.Contains(lines[6], "pane-2") {
		t.Errorf("expected spawned pane-2 in result: %s", lines[6])
	}

	// 8. Check close_pane tool call
	if !strings.Contains(lines[7], "closed successfully") || bridge.closedPaneID != "pane-2" {
		t.Errorf("expected close_pane success in result: %s, closedPaneID=%s", lines[7], bridge.closedPaneID)
	}

	// 9. Check set_agent_status tool call
	if bridge.lastBadge != "🔨 Compiling" || bridge.lastState != "building" {
		t.Errorf("unexpected agent status update: badge=%s state=%s", bridge.lastBadge, bridge.lastState)
	}
}

func TestMCPServerUnknownTool(t *testing.T) {
	bridge := &mockDaemonBridge{}
	server := NewServer(bridge, "0.1.4")

	inBuf := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"unknown_tool","arguments":{}}}` + "\n")
	outBuf := &bytes.Buffer{}

	_ = server.Run(inBuf, outBuf)
	var resp struct {
		Result ToolCallResult `json:"result"`
	}
	_ = json.Unmarshal(outBuf.Bytes(), &resp)
	if !resp.Result.IsError || len(resp.Result.Content) == 0 {
		t.Fatalf("expected error result for unknown tool: %s", outBuf.String())
	}
}
