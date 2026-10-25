package bsp

import (
	"testing"
)

func TestBSPBasicOperations(t *testing.T) {
	tree := NewTree("pane-1")
	if tree.ActivePaneID != "pane-1" {
		t.Fatalf("expected active pane-1, got %s", tree.ActivePaneID)
	}

	// Split horizontally
	_, err := tree.Split("pane-1", "pane-2", SplitHorizontal, 0.5)
	if err != nil {
		t.Fatalf("unexpected error splitting: %v", err)
	}

	leaves := tree.ListLeafPanes()
	if len(leaves) != 2 {
		t.Fatalf("expected 2 leaves, got %d", len(leaves))
	}
	if tree.ActivePaneID != "pane-2" {
		t.Fatalf("expected active pane-2, got %s", tree.ActivePaneID)
	}

	// Split pane-2 vertically
	_, err = tree.Split("pane-2", "pane-3", SplitVertical, 0.5)
	if err != nil {
		t.Fatalf("unexpected error splitting pane-2: %v", err)
	}

	leaves = tree.ListLeafPanes()
	if len(leaves) != 3 {
		t.Fatalf("expected 3 leaves, got %d", len(leaves))
	}

	// Compute Layout for an 80x24 terminal
	bounds := Rect{X: 0, Y: 0, W: 80, H: 24}
	tree.ComputeLayout(bounds, 1)

	p1 := tree.FindNode("pane-1")
	p2 := tree.FindNode("pane-2")
	p3 := tree.FindNode("pane-3")

	if p1 == nil || p2 == nil || p3 == nil {
		t.Fatalf("one of the panes is nil")
	}

	// Pane 1 should occupy top half: Y=0, H ≈ 11
	if p1.Rect.Y != 0 {
		t.Errorf("pane-1 Y expected 0, got %d", p1.Rect.Y)
	}

	// Pane 2 and 3 should occupy bottom half
	if p2.Rect.Y <= p1.Rect.H {
		t.Errorf("pane-2 Y (%d) should be below pane-1 H (%d)", p2.Rect.Y, p1.Rect.H)
	}
	if p3.Rect.X <= p2.Rect.X {
		t.Errorf("pane-3 X (%d) should be to the right of pane-2 X (%d)", p3.Rect.X, p2.Rect.X)
	}

	// Test directional navigation
	down := tree.FindNeighbor("pane-1", NavDown)
	if down != "pane-2" && down != "pane-3" {
		t.Errorf("expected down neighbor to be pane-2 or pane-3, got %s", down)
	}

	right := tree.FindNeighbor("pane-2", NavRight)
	if right != "pane-3" {
		t.Errorf("expected right neighbor of pane-2 to be pane-3, got %s", right)
	}

	left := tree.FindNeighbor("pane-3", NavLeft)
	if left != "pane-2" {
		t.Errorf("expected left neighbor of pane-3 to be pane-2, got %s", left)
	}

	// Test Swap
	err = tree.Swap("pane-2", "pane-3")
	if err != nil {
		t.Fatalf("failed to swap: %v", err)
	}
	p2After := tree.FindNode("pane-2")
	if p2After.Rect.X != p3.Rect.X {
		t.Errorf("expected pane-2 to take pane-3's position")
	}

	// Test Close
	err = tree.Close("pane-3")
	if err != nil {
		t.Fatalf("failed to close pane-3: %v", err)
	}

	leaves = tree.ListLeafPanes()
	if len(leaves) != 2 {
		t.Fatalf("expected 2 leaves after closing, got %d", len(leaves))
	}
}
