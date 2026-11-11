package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// JSON-RPC 2.0 and MCP types

type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      any           `json:"id,omitempty"`
	Result  any           `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
}

type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Tool represents an MCP tool definition.
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"inputSchema"`
}

// InputSchema defines JSON Schema for tool parameters.
type InputSchema struct {
	Type       string                 `json:"type"`
	Properties map[string]PropertyDef `json:"properties"`
	Required   []string               `json:"required,omitempty"`
}

type PropertyDef struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

// ToolCallResult represents standard MCP text content response.
type ToolCallResult struct {
	Content []TextContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type TextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Server implements a Model Context Protocol (MCP) server over standard IO.
type Server struct {
	Bridge  DaemonBridge
	Version string
}

// NewServer initializes an MCP server.
func NewServer(bridge DaemonBridge, version string) *Server {
	if version == "" {
		version = "0.1.4"
	}
	return &Server{
		Bridge:  bridge,
		Version: version,
	}
}

// Run starts the JSON-RPC message processing loop on the provided reader and writer.
func (s *Server) Run(r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}

		var req JSONRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			errResp := JSONRPCResponse{
				JSONRPC: "2.0",
				Error: &JSONRPCError{
					Code:    -32700,
					Message: "Parse error",
				},
			}
			s.writeResponse(w, errResp)
			continue
		}

		resp := s.handleRequest(req)
		// Notifications have no ID and do not expect a response
		if req.ID != nil {
			s.writeResponse(w, resp)
		}
	}
	return scanner.Err()
}

func (s *Server) writeResponse(w io.Writer, resp JSONRPCResponse) {
	data, err := json.Marshal(resp)
	if err == nil {
		data = append(data, '\n')
		_, _ = w.Write(data)
	}
}

func (s *Server) handleRequest(req JSONRPCRequest) JSONRPCResponse {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
	}

	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "dajimux",
				"version": s.Version,
			},
		}

	case "notifications/initialized":
		// No response required for notification

	case "ping":
		resp.Result = map[string]any{}

	case "tools/list":
		resp.Result = map[string]any{
			"tools": s.listTools(),
		}

	case "tools/call":
		var callParams struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &callParams); err != nil {
			resp.Error = &JSONRPCError{
				Code:    -32602,
				Message: "Invalid params",
			}
			return resp
		}

		res, err := s.callTool(callParams.Name, callParams.Arguments)
		if err != nil {
			resp.Result = ToolCallResult{
				IsError: true,
				Content: []TextContent{
					{Type: "text", Text: fmt.Sprintf("Error: %v", err)},
				},
			}
		} else {
			resp.Result = res
		}

	default:
		resp.Error = &JSONRPCError{
			Code:    -32601,
			Message: fmt.Sprintf("Method not found: %s", req.Method),
		}
	}

	return resp
}

func (s *Server) listTools() []Tool {
	return []Tool{
		{
			Name:        "list_panes",
			Description: "List all active dajimux sessions, workspaces, and pane IDs with bounding boxes, working directories, and agent statuses.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"session": {Type: "string", Description: "Optional session name to filter by (lists all if empty)"},
				},
			},
		},
		{
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
		},
		{
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
		},
		{
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
		},
		{
			Name:        "close_pane",
			Description: "Close and terminate a specific terminal pane by its ID, rebalancing the workspace layout.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"pane_id": {Type: "string", Description: "Target pane ID to close (e.g. 'pane-2')"},
				},
				Required: []string{"pane_id"},
			},
		},
		{
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
		},
	}
}

func (s *Server) callTool(name string, args json.RawMessage) (ToolCallResult, error) {
	switch name {
	case "list_panes":
		var params struct {
			Session string `json:"session"`
		}
		_ = json.Unmarshal(args, &params)

		panes, err := s.Bridge.ListPanes(params.Session)
		if err != nil {
			return ToolCallResult{}, err
		}

		data, _ := json.MarshalIndent(panes, "", "  ")
		return ToolCallResult{
			Content: []TextContent{
				{Type: "text", Text: string(data)},
			},
		}, nil

	case "read_pane":
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

		lines, err := s.Bridge.ReadPane(params.PaneID, params.Lines)
		if err != nil {
			return ToolCallResult{}, err
		}

		return ToolCallResult{
			Content: []TextContent{
				{Type: "text", Text: strings.Join(lines, "\n")},
			},
		}, nil

	case "send_command":
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

		if err := s.Bridge.SendCommand(params.PaneID, params.Command, pressEnter); err != nil {
			return ToolCallResult{}, err
		}

		return ToolCallResult{
			Content: []TextContent{
				{Type: "text", Text: fmt.Sprintf("Command sent to %s successfully.", params.PaneID)},
			},
		}, nil

	case "spawn_pane":
		var params struct {
			Session   string `json:"session"`
			Direction string `json:"direction"`
			Cwd       string `json:"cwd"`
			Command   string `json:"command"`
		}
		_ = json.Unmarshal(args, &params)

		paneID, err := s.Bridge.SpawnPane(params.Session, params.Direction, params.Cwd, params.Command)
		if err != nil {
			return ToolCallResult{}, err
		}

		return ToolCallResult{
			Content: []TextContent{
				{Type: "text", Text: fmt.Sprintf("Spawned pane %s successfully.", paneID)},
			},
		}, nil

	case "close_pane":
		var params struct {
			PaneID string `json:"pane_id"`
		}
		if err := json.Unmarshal(args, &params); err != nil || params.PaneID == "" {
			return ToolCallResult{}, fmt.Errorf("pane_id is required")
		}

		if err := s.Bridge.ClosePane(params.PaneID); err != nil {
			return ToolCallResult{}, err
		}

		return ToolCallResult{
			Content: []TextContent{
				{Type: "text", Text: fmt.Sprintf("Pane %s closed successfully.", params.PaneID)},
			},
		}, nil

	case "set_agent_status":
		var params struct {
			PaneID string `json:"pane_id"`
			State  string `json:"state"`
			Badge  string `json:"badge"`
		}
		if err := json.Unmarshal(args, &params); err != nil || params.PaneID == "" {
			return ToolCallResult{}, fmt.Errorf("pane_id is required")
		}

		if err := s.Bridge.SetAgentStatus(params.PaneID, params.State, params.Badge); err != nil {
			return ToolCallResult{}, err
		}

		return ToolCallResult{
			Content: []TextContent{
				{Type: "text", Text: fmt.Sprintf("Status updated for pane %s: state=%q, badge=%q", params.PaneID, params.State, params.Badge)},
			},
		}, nil

	default:
		return ToolCallResult{}, fmt.Errorf("unknown tool: %s", name)
	}
}
