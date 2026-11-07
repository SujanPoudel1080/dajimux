package client

import (
	"testing"
)

func TestParseSGREvent(t *testing.T) {
	// Left click down at (20, 10)
	evt1, ok := ParseSGREvent([]byte("\033[<0;20;10M"))
	if !ok || evt1 == nil {
		t.Fatalf("expected successful parse of left click down")
	}
	if evt1.Button != MouseBtnLeft || evt1.X != 20 || evt1.Y != 10 || !evt1.IsDown || evt1.IsDrag {
		t.Errorf("unexpected event 1: %+v", evt1)
	}

	// Left click release at (20, 10)
	evt2, ok := ParseSGREvent([]byte("\033[<0;20;10m"))
	if !ok || evt2 == nil {
		t.Fatalf("expected successful parse of left click release")
	}
	if evt2.Button != MouseBtnLeft || evt2.IsDown {
		t.Errorf("unexpected event 2: %+v", evt2)
	}

	// Mouse drag at (25, 12)
	evt3, ok := ParseSGREvent([]byte("\033[<32;25;12M"))
	if !ok || evt3 == nil {
		t.Fatalf("expected successful parse of drag event")
	}
	if evt3.Button != MouseBtnLeft || !evt3.IsDrag || evt3.X != 25 || evt3.Y != 12 {
		t.Errorf("unexpected drag event: %+v", evt3)
	}

	// Scroll wheel up at (40, 5)
	evt4, ok := ParseSGREvent([]byte("\033[<64;40;5M"))
	if !ok || evt4 == nil {
		t.Fatalf("expected successful parse of wheel up")
	}
	if evt4.Button != MouseWheelUp || evt4.X != 40 || evt4.Y != 5 {
		t.Errorf("unexpected wheel up event: %+v", evt4)
	}

	// Scroll wheel down at (40, 5)
	evt5, ok := ParseSGREvent([]byte("\033[<65;40;5M"))
	if !ok || evt5 == nil {
		t.Fatalf("expected successful parse of wheel down")
	}
	if evt5.Button != MouseWheelDown || evt5.X != 40 || evt5.Y != 5 {
		t.Errorf("unexpected wheel down event: %+v", evt5)
	}

	// Invalid sequences
	if _, ok := ParseSGREvent([]byte("invalid")); ok {
		t.Errorf("expected false for invalid sequence")
	}
	if _, ok := ParseSGREvent([]byte("\033[0;20;10M")); ok {
		t.Errorf("expected false for missing '<'")
	}
}

func TestFindWorkspaceFromHeaderClick(t *testing.T) {
	c := NewClient("", "main")
	c.lastState.Mode = "NORMAL"
	c.lastState.SessionName = "main"
	c.lastState.ActiveWorkspace = 1
	c.lastState.TotalWorkspaces = 4

	// Header startCol: 1 + len(" NORMAL ") + len(" [dajimux:main] ")
	// = 1 + 8 + 16 = 25
	// WS 1 (active, width 4): cols 25..28
	// WS 2 (inactive, width 3): cols 29..31
	// WS 3 (inactive, width 3): cols 32..34
	// WS 4 (inactive, width 3): cols 35..37

	if ws := c.findWorkspaceFromHeaderClick(26); ws != 1 {
		t.Errorf("expected click at 26 to match WS 1, got %d", ws)
	}
	if ws := c.findWorkspaceFromHeaderClick(30); ws != 2 {
		t.Errorf("expected click at 30 to match WS 2, got %d", ws)
	}
	if ws := c.findWorkspaceFromHeaderClick(33); ws != 3 {
		t.Errorf("expected click at 33 to match WS 3, got %d", ws)
	}
	if ws := c.findWorkspaceFromHeaderClick(36); ws != 4 {
		t.Errorf("expected click at 36 to match WS 4, got %d", ws)
	}
	if ws := c.findWorkspaceFromHeaderClick(10); ws != 0 {
		t.Errorf("expected click at 10 (mode badge) to match 0, got %d", ws)
	}
}
