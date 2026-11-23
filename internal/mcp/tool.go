package mcp

import "encoding/json"

// ToolHandler defines the interface that all MCP tool implementations must satisfy.
type ToolHandler interface {
	// Definition returns the tool metadata and JSON Schema.
	Definition() Tool

	// Execute processes the incoming tool call arguments and invokes the daemon bridge.
	Execute(bridge DaemonBridge, args json.RawMessage) (ToolCallResult, error)
}
