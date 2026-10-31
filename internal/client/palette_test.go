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
