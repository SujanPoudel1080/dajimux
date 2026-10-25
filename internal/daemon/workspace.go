package daemon

import (
	"fmt"
	"sync"

	"github.com/SujanPoudel1080/dajimux/internal/bsp"
)

// Workspace represents a distinct virtual desktop layout of panes.
type Workspace struct {
	mu     sync.RWMutex
	ID     int
	Name   string
	Tree   *bsp.Tree
	Bounds bsp.Rect
}

// NewWorkspace creates a workspace initialized with an initial pane.
func NewWorkspace(id int, name string, initialPaneID string) *Workspace {
	if name == "" {
		name = fmt.Sprintf("ws-%d", id)
	}
	return &Workspace{
		ID:   id,
		Name: name,
		Tree: bsp.NewTree(initialPaneID),
	}
}

// Layout recalculates bounding boxes for all panes within bounds, leaving space for borders.
func (w *Workspace) Layout(cols, rows int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.Bounds = bsp.Rect{
		X: 0,
		Y: 1, // Row 0 reserved for status bar / tab bar
		W: cols,
		H: rows - 2, // Row rows-1 reserved for footer / agent status line
	}

	if w.Bounds.H < 2 {
		w.Bounds.H = 2
	}
	if w.Bounds.W < 2 {
		w.Bounds.W = 2
	}

	w.Tree.ComputeLayout(w.Bounds, 1)
}

// ActivePaneID returns the ID of the currently focused pane.
func (w *Workspace) ActivePaneID() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.Tree.ActivePaneID
}

// SetActivePane updates the focused pane.
func (w *Workspace) SetActivePane(paneID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.Tree.ActivePaneID = paneID
}
