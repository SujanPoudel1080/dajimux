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
	Bridge   DaemonBridge
	Registry *ToolRegistry
	Version  string
}

// NewServer initializes an MCP server with the default tool registry.
func NewServer(bridge DaemonBridge, version string) *Server {
	return NewServerWithRegistry(bridge, DefaultToolRegistry(), version)
}

// NewServerWithRegistry initializes an MCP server with a custom tool registry.
func NewServerWithRegistry(bridge DaemonBridge, registry *ToolRegistry, version string) *Server {
	if version == "" {
		version = "0.1.4"
	}
	if registry == nil {
		registry = DefaultToolRegistry()
	}
	return &Server{
		Bridge:   bridge,
		Registry: registry,
		Version:  version,
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
			"tools": s.Registry.List(),
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

		res, err := s.Registry.Execute(callParams.Name, s.Bridge, callParams.Arguments)
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
