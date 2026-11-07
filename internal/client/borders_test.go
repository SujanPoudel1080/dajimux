package client

import (
	"strings"
	"testing"

	"github.com/SujanPoudel1080/dajimux/internal/protocol"
)

func TestRenderBorders(t *testing.T) {
	// Single pane -> no internal borders rendered
	single := []protocol.PaneInfo{
		{ID: "pane-1", Title: "shell", X: 0, Y: 1, W: 80, H: 22, IsActive: true},
	}
	resSingle := RenderBorders(single, 80, 24, false)
	if resSingle != "" {
		t.Errorf("expected empty string for single pane, got %q", resSingle)
	}

	// Two split panes
	two := []protocol.PaneInfo{
		{ID: "pane-1", Title: "backend", X: 0, Y: 1, W: 39, H: 22, IsActive: true, AgentBadge: "🟢 Serving"},
		{ID: "pane-2", Title: "frontend", X: 40, Y: 1, W: 40, H: 22, IsActive: false},
	}
	resTwo := RenderBorders(two, 80, 24, false)
	if !strings.Contains(resTwo, "backend") {
		t.Errorf("expected backend title in border output")
	}
	if !strings.Contains(resTwo, "🟢 Serving") {
		t.Errorf("expected agent badge in border output")
	}
	if !strings.Contains(resTwo, "frontend") {
		t.Errorf("expected frontend title in border output")
	}

	// Monocle zoomed -> no borders
	resMonocle := RenderBorders(two, 80, 24, true)
	if resMonocle != "" {
		t.Errorf("expected empty string in monocle zoom mode")
	}

	// Custom theme colors
	customActive := "\033[38;5;46m" // Green
	customInactive := "\033[38;5;238m"
	resThemed := RenderBordersWithTheme(two, 80, 24, false, customActive, customInactive)
	if !strings.Contains(resThemed, customActive) {
		t.Errorf("expected custom active color in border output")
	}
	if !strings.Contains(resThemed, customInactive) {
		t.Errorf("expected custom inactive color in border output")
	}
}
