package mcp

import (
	"encoding/json"
	"testing"
)

type dummyCustomTool struct{}

func (d *dummyCustomTool) Definition() Tool {
	return Tool{
		Name:        "custom_echo",
		Description: "Echoes input back",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertyDef{
				"msg": {Type: "string", Description: "Message to echo"},
			},
			Required: []string{"msg"},
		},
	}
}

func (d *dummyCustomTool) Execute(bridge DaemonBridge, args json.RawMessage) (ToolCallResult, error) {
	var params struct {
		Msg string `json:"msg"`
	}
	_ = json.Unmarshal(args, &params)
	return ToolCallResult{
		Content: []TextContent{
			{Type: "text", Text: "Echo: " + params.Msg},
		},
	}, nil
}

func TestToolRegistry(t *testing.T) {
	reg := NewToolRegistry()
	if len(reg.List()) != 0 {
		t.Fatalf("expected empty registry, got %d", len(reg.List()))
	}

	reg.Register(&dummyCustomTool{})
	tools := reg.List()
	if len(tools) != 1 || tools[0].Name != "custom_echo" {
		t.Fatalf("unexpected tools list: %+v", tools)
	}

	// Test successful execution
	res, err := reg.Execute("custom_echo", nil, json.RawMessage(`{"msg":"hello world"}`))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(res.Content) != 1 || res.Content[0].Text != "Echo: hello world" {
		t.Errorf("unexpected execute result: %+v", res)
	}

	// Test unknown tool execution
	_, err = reg.Execute("non_existent", nil, nil)
	if err == nil {
		t.Fatalf("expected error for unknown tool, got nil")
	}
}

func TestDefaultToolRegistry(t *testing.T) {
	reg := DefaultToolRegistry()
	tools := reg.List()
	if len(tools) != 8 {
		t.Fatalf("expected 8 built-in tools, got %d", len(tools))
	}

	expectedNames := []string{
		"list_panes",
		"read_pane",
		"send_command",
		"spawn_pane",
		"close_pane",
		"rename_workspace",
		"rename_session",
		"set_agent_status",
	}

	for i, name := range expectedNames {
		if tools[i].Name != name {
			t.Errorf("expected tool[%d] to be %q, got %q", i, name, tools[i].Name)
		}
	}
}
