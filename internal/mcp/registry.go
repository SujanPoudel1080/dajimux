package mcp

import (
	"encoding/json"
	"fmt"
	"sync"
)

// ToolRegistry maintains registered MCP tools and routes execution calls.
type ToolRegistry struct {
	mu       sync.RWMutex
	handlers map[string]ToolHandler
	order    []string
}

// NewToolRegistry allocates an empty ToolRegistry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		handlers: make(map[string]ToolHandler),
		order:    make([]string, 0),
	}
}

// Register adds a tool handler to the registry.
func (r *ToolRegistry) Register(handler ToolHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := handler.Definition().Name
	if _, exists := r.handlers[name]; !exists {
		r.order = append(r.order, name)
	}
	r.handlers[name] = handler
}

// List returns the schemas of all registered tools in registration order.
func (r *ToolRegistry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tools := make([]Tool, 0, len(r.order))
	for _, name := range r.order {
		if h, ok := r.handlers[name]; ok {
			tools = append(tools, h.Definition())
		}
	}
	return tools
}

// Execute routes a tool call to the matching registered handler.
func (r *ToolRegistry) Execute(name string, bridge DaemonBridge, args json.RawMessage) (ToolCallResult, error) {
	r.mu.RLock()
	handler, ok := r.handlers[name]
	r.mu.RUnlock()

	if !ok {
		return ToolCallResult{}, fmt.Errorf("unknown tool: %s", name)
	}

	return handler.Execute(bridge, args)
}

// DefaultToolRegistry constructs a registry preloaded with all built-in dajimux tools.
func DefaultToolRegistry() *ToolRegistry {
	r := NewToolRegistry()
	r.Register(&ListPanesTool{})
	r.Register(&ReadPaneTool{})
	r.Register(&SendCommandTool{})
	r.Register(&SpawnPaneTool{})
	r.Register(&ClosePaneTool{})
	r.Register(&RenameWorkspaceTool{})
	r.Register(&RenameSessionTool{})
	r.Register(&SetAgentStatusTool{})
	return r
}
