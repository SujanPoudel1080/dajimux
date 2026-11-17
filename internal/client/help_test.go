package client

import (
	"strings"
	"testing"
)

func TestHelpModalLifecycle(t *testing.T) {
	h := NewHelpModal()
	if h.IsVisible() {
		t.Errorf("expected new modal to be hidden")
	}

	h.Show()
	if !h.IsVisible() {
		t.Errorf("expected modal to be visible after Show()")
	}

	h.Hide()
	if h.IsVisible() {
		t.Errorf("expected modal to be hidden after Hide()")
	}

	state := h.Toggle()
	if !state || !h.IsVisible() {
		t.Errorf("expected modal to be visible after Toggle()")
	}

	state = h.Toggle()
	if state || h.IsVisible() {
		t.Errorf("expected modal to be hidden after second Toggle()")
	}
}

func TestHelpModalRender(t *testing.T) {
	h := NewHelpModal()
	output := h.Render(80, 24)

	if !strings.Contains(output, "dajimux Quick Reference") {
		t.Errorf("render output should contain title")
	}
	if !strings.Contains(output, "MODES & BASICS") {
		t.Errorf("render output should contain section headers")
	}
	if !strings.Contains(output, "Press Esc, q, or ? to dismiss") {
		t.Errorf("render output should contain dismiss prompt")
	}
}
