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
	p.MoveUp(len(p.Items))
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

func TestPaletteScrollingAndWrapAround(t *testing.T) {
	p := NewPalette() // Has 16 default commands (7 actions + 9 workspaces)
	items := p.Filter()
	total := len(items)
	if total < 10 {
		t.Fatalf("expected at least 10 items for scrolling test, got %d", total)
	}

	maxVisible := 8

	// Initially at top
	p.AdjustScroll(maxVisible, total)
	if p.ScrollOffset != 0 || p.SelectedIndex != 0 {
		t.Fatalf("expected scroll 0 and sel 0, got scroll %d, sel %d", p.ScrollOffset, p.SelectedIndex)
	}

	// Move down 8 times (reaches index 8, the 9th item)
	for i := 0; i < 8; i++ {
		p.MoveDown(total)
	}
	if p.SelectedIndex != 8 {
		t.Fatalf("expected selected index 8, got %d", p.SelectedIndex)
	}

	// Adjust scroll: index 8 must be inside [ScrollOffset, ScrollOffset+8)
	p.AdjustScroll(maxVisible, total)
	if p.ScrollOffset < 1 {
		t.Errorf("expected ScrollOffset >= 1 when SelectedIndex=8, got %d", p.ScrollOffset)
	}
	if p.SelectedIndex < p.ScrollOffset || p.SelectedIndex >= p.ScrollOffset+maxVisible {
		t.Errorf("selected index %d must be inside visible window [%d, %d)", p.SelectedIndex, p.ScrollOffset, p.ScrollOffset+maxVisible)
	}

	// Move all the way to the last item
	for i := p.SelectedIndex; i < total-1; i++ {
		p.MoveDown(total)
	}
	if p.SelectedIndex != total-1 {
		t.Fatalf("expected selected index %d (last item), got %d", total-1, p.SelectedIndex)
	}

	// Moving down from the last item must wrap around to index 0
	p.MoveDown(total)
	if p.SelectedIndex != 0 {
		t.Fatalf("expected wrap-around to 0, got %d", p.SelectedIndex)
	}
	p.AdjustScroll(maxVisible, total)
	if p.ScrollOffset != 0 {
		t.Fatalf("expected scroll offset reset to 0, got %d", p.ScrollOffset)
	}

	// Moving up from index 0 must wrap around to the last item
	p.MoveUp(total)
	if p.SelectedIndex != total-1 {
		t.Fatalf("expected wrap-around to %d, got %d", total-1, p.SelectedIndex)
	}
	p.AdjustScroll(maxVisible, total)
	if p.SelectedIndex < p.ScrollOffset || p.SelectedIndex >= p.ScrollOffset+maxVisible {
		t.Errorf("selected index %d must be inside visible window [%d, %d)", p.SelectedIndex, p.ScrollOffset, p.ScrollOffset+maxVisible)
	}
}
