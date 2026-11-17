package client

import (
	"strings"
	"testing"
)

func TestScrollbackViewerNavigation(t *testing.T) {
	v := NewScrollbackViewer()
	if v.Active() {
		t.Errorf("expected new viewer to be inactive")
	}

	lines := make([]string, 100)
	for i := 0; i < 100; i++ {
		lines[i] = "line"
	}
	v.SetLines(lines)

	if !v.Active() {
		t.Errorf("expected viewer to be active after SetLines")
	}

	rows := 22 // viewport height = rows - 2 = 20
	max := v.MaxOffset(rows)
	if max != 80 {
		t.Errorf("expected max offset 80, got %d", max)
	}

	v.ScrollUp(5, rows)
	if v.Offset() != 5 {
		t.Errorf("expected offset 5, got %d", v.Offset())
	}

	v.ScrollDown(2)
	if v.Offset() != 3 {
		t.Errorf("expected offset 3, got %d", v.Offset())
	}

	v.ScrollTop(rows)
	if v.Offset() != 80 {
		t.Errorf("expected offset 80, got %d", v.Offset())
	}

	v.ScrollBottom()
	if v.Offset() != 0 {
		t.Errorf("expected offset 0, got %d", v.Offset())
	}

	v.Clear()
	if v.Active() {
		t.Errorf("expected viewer to be inactive after Clear")
	}
}

func TestScrollbackViewerVisibleTextAndRender(t *testing.T) {
	v := NewScrollbackViewer()
	lines := []string{"first", "second", "third", "fourth", "fifth"}
	v.SetLines(lines)

	rows := 5 // viewHeight = 3
	text, count := v.VisibleText(rows)
	if count != 3 {
		t.Errorf("expected 3 lines, got %d", count)
	}
	if text != "third\nfourth\nfifth" {
		t.Errorf("unexpected text: %q", text)
	}

	renderOutput := v.Render(80, rows)
	if !strings.Contains(renderOutput, "SCROLLBACK") {
		t.Errorf("render output should contain SCROLLBACK banner")
	}

	v.SetToast("Copied!")
	toastOutput := v.Render(80, rows)
	if !strings.Contains(toastOutput, "Copied!") {
		t.Errorf("render output should contain toast message")
	}
}
