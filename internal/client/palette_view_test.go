package client

import (
	"strings"
	"testing"
)

func TestPaletteOverlayRenderAndClear(t *testing.T) {
	overlay := NewPaletteOverlay()
	pal := NewPalette()

	rendered := overlay.Render(pal, 80, 24)
	if !strings.Contains(rendered, "Command Palette") {
		t.Fatalf("expected rendered overlay to contain 'Command Palette', got: %s", rendered)
	}

	clearSeq, ok := overlay.Clear()
	if !ok || clearSeq == "" {
		t.Fatalf("expected clear sequence to be returned after rendering")
	}

	// Second clear should return false because dimensions were reset
	_, ok2 := overlay.Clear()
	if ok2 {
		t.Fatalf("expected second clear to return false")
	}
}
