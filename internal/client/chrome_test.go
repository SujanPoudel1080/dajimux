package client

import (
	"strings"
	"testing"

	"github.com/SujanPoudel1080/dajimux/internal/config"
	"github.com/SujanPoudel1080/dajimux/internal/protocol"
)

func TestChromeRendererOutput(t *testing.T) {
	cfg := config.DefaultConfig()
	renderer := NewChromeRenderer(cfg, nil)

	state := protocol.LayoutStatePayload{
		SessionName:     "test-sess",
		Mode:            protocol.ModeInsert,
		ActiveWorkspace: 1,
		TotalWorkspaces: 3,
		Workspaces: []protocol.WorkspaceMeta{
			{ID: 1, Name: "main"},
			{ID: 2, Name: "dev"},
			{ID: 3, Name: "test"},
		},
		Panes: []protocol.PaneInfo{
			{ID: "p1", X: 0, Y: 1, W: 80, H: 22, IsActive: true},
		},
	}

	out, sizeChanged := renderer.Render(state, 80, 24, false, "", nil)
	if !sizeChanged {
		t.Errorf("expected sizeChanged to be true on first render")
	}
	if !strings.Contains(out, "test-sess") {
		t.Errorf("expected output to contain session name")
	}
	if !strings.Contains(out, "INSERT") {
		t.Errorf("expected output to contain INSERT mode badge")
	}

	// Workspace click detection
	ws := renderer.WorkspaceFromHeaderClick(28, state, false)
	if ws < 0 || ws > 3 {
		t.Errorf("expected valid workspace click result, got %d", ws)
	}
}
