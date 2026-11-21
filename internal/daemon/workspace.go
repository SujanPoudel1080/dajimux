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

// Rename updates the display name of this workspace.
func (w *Workspace) Rename(newName string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if newName != "" {
		w.Name = newName
	}
}

// LayoutType returns the current layout preset of this workspace.
func (w *Workspace) LayoutType() bsp.LayoutType {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.Tree == nil {
		return bsp.LayoutBSP
	}
	return w.Tree.LayoutType
}

// HasBorders returns whether the workspace requires rendered borders.
func (w *Workspace) HasBorders() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.Tree == nil {
		return false
	}
	return w.Tree.HasBorders()
}

// IsMonocle returns whether the workspace is currently in fullscreen zoom mode.
func (w *Workspace) IsMonocle() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.Tree == nil {
		return false
	}
	return w.Tree.IsMonocle()
}

// MonoclePaneID returns the ID of the pane in monocle mode, or empty if not in monocle mode.
func (w *Workspace) MonoclePaneID() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.Tree == nil {
		return ""
	}
	return w.Tree.MonoclePaneID
}

// LeafPanes returns the ordered list of leaf pane IDs in this workspace.
func (w *Workspace) LeafPanes() []string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.Tree == nil {
		return nil
	}
	return w.Tree.ListLeafPanes()
}

// LeafCount returns the number of active panes in this workspace.
func (w *Workspace) LeafCount() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.Tree == nil {
		return 0
	}
	return w.Tree.LeafCount()
}

// ContainsPane returns true if the workspace contains the specified pane.
func (w *Workspace) ContainsPane(paneID string) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.Tree == nil {
		return false
	}
	return w.Tree.FindNode(paneID) != nil
}

// PaneGeometry returns the bounding box and inner viewport dimensions for a pane.
func (w *Workspace) PaneGeometry(paneID string) (bsp.Rect, int, int, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.Tree == nil {
		return bsp.Rect{}, 0, 0, false
	}
	return w.Tree.PaneGeometry(paneID)
}

// Split splits a target pane in the specified direction.
func (w *Workspace) Split(targetPaneID, newPaneID string, dir bsp.SplitDirection) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.Tree == nil {
		return fmt.Errorf("tree is nil")
	}
	_, err := w.Tree.Split(targetPaneID, newPaneID, dir, 0.5)
	return err
}

// SplitAuto automatically splits a target pane based on aspect ratio.
func (w *Workspace) SplitAuto(targetPaneID, newPaneID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.Tree == nil {
		return fmt.Errorf("tree is nil")
	}
	_, err := w.Tree.SplitAuto(targetPaneID, newPaneID)
	return err
}

// ClosePane removes a pane from the workspace tree.
func (w *Workspace) ClosePane(paneID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.Tree == nil {
		return fmt.Errorf("tree is nil")
	}
	return w.Tree.Close(paneID)
}

// IsEmpty returns true if there are no panes in the workspace tree.
func (w *Workspace) IsEmpty() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.Tree == nil || w.Tree.Root == nil
}

// FindNeighbor locates the spatial neighbor pane in the given direction.
func (w *Workspace) FindNeighbor(paneID string, dir bsp.NavDirection) string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.Tree == nil {
		return ""
	}
	return w.Tree.FindNeighbor(paneID, dir)
}

// SwapNext swaps the active pane with the next leaf pane in sequence.
func (w *Workspace) SwapNext() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.Tree == nil {
		return fmt.Errorf("tree is nil")
	}
	return w.Tree.SwapNext()
}

// SwapDirection swaps the active pane with the spatial neighbor in the given direction.
func (w *Workspace) SwapDirection(dir bsp.NavDirection) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.Tree == nil {
		return fmt.Errorf("tree is nil")
	}
	return w.Tree.SwapDirection(dir)
}

// AdjustRatio adjusts the split ratio for the focused pane.
func (w *Workspace) AdjustRatio(paneID string, delta float64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.Tree != nil {
		w.Tree.AdjustRatio(paneID, delta)
	}
}

// ToggleMonocle toggles fullscreen monocle zoom for the focused pane.
func (w *Workspace) ToggleMonocle(paneID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.Tree != nil {
		w.Tree.ToggleMonocle(paneID)
	}
}

// CycleLayout cycles to the next layout preset.
func (w *Workspace) CycleLayout() bsp.LayoutType {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.Tree == nil {
		return bsp.LayoutBSP
	}
	return w.Tree.CycleLayout()
}

// SetLayoutType sets the layout preset for this workspace.
func (w *Workspace) SetLayoutType(lt bsp.LayoutType) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.Tree != nil {
		w.Tree.SetLayoutType(lt)
	}
}

// InsertPane inserts a pane into the workspace tree.
func (w *Workspace) InsertPane(paneID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.Tree == nil {
		w.Tree = bsp.NewTree(paneID)
		return nil
	}
	_, err := w.Tree.InsertPane(paneID)
	return err
}

// SetRootPane resets the tree with a single root pane.
func (w *Workspace) SetRootPane(paneID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.Tree = bsp.NewTree(paneID)
}
