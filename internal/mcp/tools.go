package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ListPanesTool lists active sessions, workspaces, and panes.
type ListPanesTool struct{}

func (t *ListPanesTool) Definition() Tool {
	return Tool{
		Name:        "list_panes",
		Description: "List all active dajimux sessions, workspaces, and pane IDs with bounding boxes, working directories, and agent statuses.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertyDef{
				"session": {Type: "string", Description: "Optional session name to filter by (lists all if empty)"},
			},
		},
	}
}

func (t *ListPanesTool) Execute(bridge DaemonBridge, args json.RawMessage) (ToolCallResult, error) {
	var params struct {
		Session string `json:"session"`
	}
	_ = json.Unmarshal(args, &params)

	panes, err := bridge.ListPanes(params.Session)
	if err != nil {
		return ToolCallResult{}, err
	}

	data, _ := json.MarshalIndent(panes, "", "  ")
	return ToolCallResult{
		Content: []TextContent{
			{Type: "text", Text: string(data)},
		},
	}, nil
}

// ReadPaneTool captures recent output lines from a pane.
type ReadPaneTool struct{}

func (t *ReadPaneTool) Definition() Tool {
	return Tool{
		Name:        "read_pane",
		Description: "Capture recent output or scrollback lines from a specific dajimux pane.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertyDef{
				"pane_id": {Type: "string", Description: "Target pane ID (e.g. 'pane-1')"},
				"lines":   {Type: "integer", Description: "Number of lines to capture (default 50)"},
			},
			Required: []string{"pane_id"},
		},
	}
}

func (t *ReadPaneTool) Execute(bridge DaemonBridge, args json.RawMessage) (ToolCallResult, error) {
	var params struct {
		PaneID string `json:"pane_id"`
		Lines  int    `json:"lines"`
	}
	if err := json.Unmarshal(args, &params); err != nil || params.PaneID == "" {
		return ToolCallResult{}, fmt.Errorf("pane_id is required")
	}
	if params.Lines <= 0 {
		params.Lines = 50
	}

	lines, err := bridge.ReadPane(params.PaneID, params.Lines)
	if err != nil {
		return ToolCallResult{}, err
	}

	return ToolCallResult{
		Content: []TextContent{
			{Type: "text", Text: strings.Join(lines, "\n")},
		},
	}, nil
}

// SendCommandTool sends input or commands to a pane.
type SendCommandTool struct{}

func (t *SendCommandTool) Definition() Tool {
	return Tool{
		Name:        "send_command",
		Description: "Inject a shell command or keystrokes into a target pane.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertyDef{
				"pane_id":     {Type: "string", Description: "Target pane ID"},
				"command":     {Type: "string", Description: "Shell command or input text to send"},
				"press_enter": {Type: "boolean", Description: "Whether to append a newline to execute (default true)"},
			},
			Required: []string{"pane_id", "command"},
		},
	}
}

func (t *SendCommandTool) Execute(bridge DaemonBridge, args json.RawMessage) (ToolCallResult, error) {
	var params struct {
		PaneID     string `json:"pane_id"`
		Command    string `json:"command"`
		PressEnter *bool  `json:"press_enter"`
	}
	if err := json.Unmarshal(args, &params); err != nil || params.PaneID == "" || params.Command == "" {
		return ToolCallResult{}, fmt.Errorf("pane_id and command are required")
	}

	pressEnter := true
	if params.PressEnter != nil {
		pressEnter = *params.PressEnter
	}

	if err := bridge.SendCommand(params.PaneID, params.Command, pressEnter); err != nil {
		return ToolCallResult{}, err
	}

	return ToolCallResult{
		Content: []TextContent{
			{Type: "text", Text: fmt.Sprintf("Command sent to %s successfully.", params.PaneID)},
		},
	}, nil
}

// SpawnPaneTool creates a new pane in a session workspace.
type SpawnPaneTool struct{}

func (t *SpawnPaneTool) Definition() Tool {
	return Tool{
		Name:        "spawn_pane",
		Description: "Dynamically split and spawn a new terminal pane in a session workspace.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertyDef{
				"session":   {Type: "string", Description: "Target session name (default 'main')"},
				"direction": {Type: "string", Description: "Split direction: 'vertical' (side-by-side) or 'horizontal' (top/bottom)", Enum: []string{"vertical", "horizontal"}},
				"cwd":       {Type: "string", Description: "Working directory for the new pane"},
				"command":   {Type: "string", Description: "Initial shell command to run in new pane"},
			},
		},
	}
}

func (t *SpawnPaneTool) Execute(bridge DaemonBridge, args json.RawMessage) (ToolCallResult, error) {
	var params struct {
		Session   string `json:"session"`
		Direction string `json:"direction"`
		Cwd       string `json:"cwd"`
		Command   string `json:"command"`
	}
	_ = json.Unmarshal(args, &params)

	paneID, err := bridge.SpawnPane(params.Session, params.Direction, params.Cwd, params.Command)
	if err != nil {
		return ToolCallResult{}, err
	}

	return ToolCallResult{
		Content: []TextContent{
			{Type: "text", Text: fmt.Sprintf("Spawned pane %s successfully.", paneID)},
		},
	}, nil
}

// ClosePaneTool terminates a specific pane by ID.
type ClosePaneTool struct{}

func (t *ClosePaneTool) Definition() Tool {
	return Tool{
		Name:        "close_pane",
		Description: "Close and terminate a specific terminal pane by its ID, rebalancing the workspace layout.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertyDef{
				"pane_id": {Type: "string", Description: "Target pane ID to close (e.g. 'pane-2')"},
			},
			Required: []string{"pane_id"},
		},
	}
}

func (t *ClosePaneTool) Execute(bridge DaemonBridge, args json.RawMessage) (ToolCallResult, error) {
	var params struct {
		PaneID string `json:"pane_id"`
	}
	if err := json.Unmarshal(args, &params); err != nil || params.PaneID == "" {
		return ToolCallResult{}, fmt.Errorf("pane_id is required")
	}

	if err := bridge.ClosePane(params.PaneID); err != nil {
		return ToolCallResult{}, err
	}

	return ToolCallResult{
		Content: []TextContent{
			{Type: "text", Text: fmt.Sprintf("Pane %s closed successfully.", params.PaneID)},
		},
	}, nil
}

// RenameWorkspaceTool updates a workspace's display name.
type RenameWorkspaceTool struct{}

func (t *RenameWorkspaceTool) Definition() Tool {
	return Tool{
		Name:        "rename_workspace",
		Description: "Assign a custom display name to a workspace within a session.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertyDef{
				"session":   {Type: "string", Description: "Target session name (default active)"},
				"workspace": {Type: "integer", Description: "Target workspace number (1-9, default active)"},
				"name":      {Type: "string", Description: "New workspace display name (e.g. 'build', 'backend')"},
			},
			Required: []string{"name"},
		},
	}
}

func (t *RenameWorkspaceTool) Execute(bridge DaemonBridge, args json.RawMessage) (ToolCallResult, error) {
	var params struct {
		Session   string `json:"session"`
		Workspace int    `json:"workspace"`
		Name      string `json:"name"`
	}
	if err := json.Unmarshal(args, &params); err != nil || params.Name == "" {
		return ToolCallResult{}, fmt.Errorf("name is required")
	}

	if err := bridge.RenameWorkspace(params.Session, params.Workspace, params.Name); err != nil {
		return ToolCallResult{}, err
	}

	return ToolCallResult{
		Content: []TextContent{
			{Type: "text", Text: fmt.Sprintf("Workspace renamed to %q successfully.", params.Name)},
		},
	}, nil
}

// RenameSessionTool renames an active session.
type RenameSessionTool struct{}

func (t *RenameSessionTool) Definition() Tool {
	return Tool{
		Name:        "rename_session",
		Description: "Rename an active dajimux session.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertyDef{
				"old_name": {Type: "string", Description: "Current session name"},
				"new_name": {Type: "string", Description: "New session name"},
			},
			Required: []string{"old_name", "new_name"},
		},
	}
}

func (t *RenameSessionTool) Execute(bridge DaemonBridge, args json.RawMessage) (ToolCallResult, error) {
	var params struct {
		OldName string `json:"old_name"`
		NewName string `json:"new_name"`
	}
	if err := json.Unmarshal(args, &params); err != nil || params.OldName == "" || params.NewName == "" {
		return ToolCallResult{}, fmt.Errorf("old_name and new_name are required")
	}

	if err := bridge.RenameSession(params.OldName, params.NewName); err != nil {
		return ToolCallResult{}, err
	}

	return ToolCallResult{
		Content: []TextContent{
			{Type: "text", Text: fmt.Sprintf("Session %q renamed to %q successfully.", params.OldName, params.NewName)},
		},
	}, nil
}

// SetAgentStatusTool updates agent status and badge for a pane.
type SetAgentStatusTool struct{}

func (t *SetAgentStatusTool) Definition() Tool {
	return Tool{
		Name:        "set_agent_status",
		Description: "Update the agent state and status badge displayed in a pane's header.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertyDef{
				"pane_id": {Type: "string", Description: "Target pane ID"},
				"state":   {Type: "string", Description: "Agent state (e.g. idle, thinking, executing)"},
				"badge":   {Type: "string", Description: "Visual status badge description (e.g. '🤖 Running tests')"},
			},
			Required: []string{"pane_id"},
		},
	}
}

func (t *SetAgentStatusTool) Execute(bridge DaemonBridge, args json.RawMessage) (ToolCallResult, error) {
	var params struct {
		PaneID string `json:"pane_id"`
		State  string `json:"state"`
		Badge  string `json:"badge"`
	}
	if err := json.Unmarshal(args, &params); err != nil || params.PaneID == "" {
		return ToolCallResult{}, fmt.Errorf("pane_id is required")
	}

	if err := bridge.SetAgentStatus(params.PaneID, params.State, params.Badge); err != nil {
		return ToolCallResult{}, err
	}

	return ToolCallResult{
		Content: []TextContent{
			{Type: "text", Text: fmt.Sprintf("Status updated for pane %s: state=%q, badge=%q", params.PaneID, params.State, params.Badge)},
		},
	}, nil
}
