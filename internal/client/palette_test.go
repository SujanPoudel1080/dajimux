package client

import (
	"testing"
)

func TestPaletteFuzzyFilter(t *testing.T) {
	p := NewPalette()

	// Initial filter with empty query returns all items
	all := p.Filter()
	if len(all) != len(p.Items) {
		t.Fatalf("expected all %d items, got %d", len(p.Items), len(all))
	}

	// Filter for "vert"
	p.TypeChar('v')
	p.TypeChar('e')
	p.TypeChar('r')
	p.TypeChar('t')

	filtered := p.Filter()
	if len(filtered) != 1 {
		t.Fatalf("expected 1 match for 'vert', got %d", len(filtered))
	}
	if filtered[0].ID != "split_v" {
		t.Errorf("expected split_v, got %s", filtered[0].ID)
	}

	// Backspace and test navigation
	p.Backspace()
	p.Backspace()
	p.Backspace()
	p.Backspace()

	p.MoveDown(len(p.Items))
	if p.SelectedIndex != 1 {
		t.Errorf("expected selected index 1, got %d", p.SelectedIndex)
	}
	p.MoveUp()
	if p.SelectedIndex != 0 {
		t.Errorf("expected selected index 0, got %d", p.SelectedIndex)
	}
}

func TestPaletteWorkspaceListing(t *testing.T) {
	p := NewPaletteWithWorkspaces(6)

	// Ensure all 9 workspaces are present
	wsFound := make(map[int]bool)
	for _, item := range p.Items {
		if item.Action == ActionSwitchWorkspace {
			wsFound[item.Workspace] = true
		}
	}

	for ws := 1; ws <= 6; ws++ {
		if !wsFound[ws] {
			t.Errorf("expected workspace %d to be in palette items", ws)
		}
	}

	// Test searching for "workspace 4"
	p.Query = "workspace 4"
	matches := p.Filter()
	if len(matches) == 0 {
		t.Fatalf("expected matches for 'workspace 4', got none")
	}
	if matches[0].Workspace != 4 {
		t.Errorf("expected matched workspace 4, got %d", matches[0].Workspace)
	}

	// Test searching for "workspace 5"
	p.Query = "workspace 5"
	matches = p.Filter()
	if len(matches) == 0 {
		t.Fatalf("expected matches for 'workspace 5', got none")
	}
	if matches[0].Workspace != 5 {
		t.Errorf("expected matched workspace 5, got %d", matches[0].Workspace)
	}
}
