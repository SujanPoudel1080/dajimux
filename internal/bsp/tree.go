package bsp

import (
	"errors"
	"fmt"
	"math"
)

// SplitDirection defines whether a split is horizontal (top/bottom) or vertical (left/right).
type SplitDirection uint8

const (
	SplitHorizontal SplitDirection = iota // Sibling on bottom
	SplitVertical                         // Sibling on right
)

func (s SplitDirection) String() string {
	switch s {
	case SplitHorizontal:
		return "horizontal"
	case SplitVertical:
		return "vertical"
	default:
		return "unknown"
	}
}

// NavDirection defines spatial navigation direction (vim h/j/k/l).
type NavDirection uint8

const (
	NavLeft NavDirection = iota
	NavDown
	NavUp
	NavRight
)

// Rect represents a 2D bounding rectangle in terminal cell coordinates.
type Rect struct {
	X int // 0-indexed column
	Y int // 0-indexed row
	W int // width in cells
	H int // height in cells
}

// Center returns the geometric midpoint of the rectangle.
func (r Rect) Center() (float64, float64) {
	return float64(r.X) + float64(r.W)/2.0, float64(r.Y) + float64(r.H)/2.0
}

// Node represents an element in the binary space partitioning tree.
type Node struct {
	ID     string
	Parent *Node
	Left   *Node // Left or Top child
	Right  *Node // Right or Bottom child
	Split  SplitDirection
	Ratio  float64 // 0.0 - 1.0 (proportion allocated to Left)
	PaneID string  // Non-empty only if this node is a leaf
	Rect   Rect    // Computed layout bounds
}

// IsLeaf returns true if the node contains a terminal pane.
func (n *Node) IsLeaf() bool {
	return n.Left == nil && n.Right == nil && n.PaneID != ""
}

// Tree manages the hierarchical binary tree of panes for a workspace.
type Tree struct {
	Root           *Node
	ActivePaneID   string
	MonoclePaneID  string
	nodeCounter    int
}

// NewTree creates a new BSP tree initialized with a single root pane.
func NewTree(rootPaneID string) *Tree {
	root := &Node{
		ID:     "node-1",
		PaneID: rootPaneID,
		Ratio:  0.5,
	}
	return &Tree{
		Root:         root,
		ActivePaneID: rootPaneID,
		nodeCounter:  1,
	}
}

func (t *Tree) nextNodeID() string {
	t.nodeCounter++
	return fmt.Sprintf("node-%d", t.nodeCounter)
}

// FindNode recursively searches for a leaf node matching the pane ID.
func (t *Tree) FindNode(paneID string) *Node {
	return findNodeByPaneID(t.Root, paneID)
}

func findNodeByPaneID(current *Node, paneID string) *Node {
	if current == nil {
		return nil
	}
	if current.IsLeaf() && current.PaneID == paneID {
		return current
	}
	if left := findNodeByPaneID(current.Left, paneID); left != nil {
		return left
	}
	return findNodeByPaneID(current.Right, paneID)
}

// Split splits the targeted leaf node into two children.
func (t *Tree) Split(targetPaneID, newPaneID string, dir SplitDirection, ratio float64) (*Node, error) {
	if ratio <= 0.05 || ratio >= 0.95 {
		ratio = 0.5
	}

	target := t.FindNode(targetPaneID)
	if target == nil {
		return nil, fmt.Errorf("pane %q not found in BSP tree", targetPaneID)
	}

	// Turn target into an internal split node
	leftChild := &Node{
		ID:     t.nextNodeID(),
		Parent: target,
		PaneID: target.PaneID,
		Ratio:  0.5,
	}

	rightChild := &Node{
		ID:     t.nextNodeID(),
		Parent: target,
		PaneID: newPaneID,
		Ratio:  0.5,
	}

	target.PaneID = ""
	target.Left = leftChild
	target.Right = rightChild
	target.Split = dir
	target.Ratio = ratio

	t.ActivePaneID = newPaneID
	return rightChild, nil
}

// SplitAuto splits the target pane automatically: vertically if wide, horizontally if tall.
func (t *Tree) SplitAuto(targetPaneID, newPaneID string) (*Node, error) {
	target := t.FindNode(targetPaneID)
	if target == nil {
		return nil, fmt.Errorf("pane %q not found", targetPaneID)
	}

	dir := SplitHorizontal
	// In terminals, characters are taller than they are wide (approx ~2:1 aspect ratio).
	// If width > 2 * height, split vertical (left/right).
	if target.Rect.W > (target.Rect.H * 2) {
		dir = SplitVertical
	}
	return t.Split(targetPaneID, newPaneID, dir, 0.5)
}

// Close removes a pane from the BSP tree. The sibling collapses into the parent node.
func (t *Tree) Close(paneID string) error {
	node := t.FindNode(paneID)
	if node == nil {
		return fmt.Errorf("pane %q not found", paneID)
	}

	parent := node.Parent
	if parent == nil {
		// Closing the root pane leaves tree empty
		t.Root = nil
		t.ActivePaneID = ""
		return nil
	}

	// Identify sibling
	var sibling *Node
	if parent.Left == node {
		sibling = parent.Right
	} else {
		sibling = parent.Left
	}

	grandparent := parent.Parent
	sibling.Parent = grandparent

	if grandparent == nil {
		t.Root = sibling
	} else {
		if grandparent.Left == parent {
			grandparent.Left = sibling
		} else {
			grandparent.Right = sibling
		}
	}

	// Reset active pane if closing current
	if t.ActivePaneID == paneID {
		leaves := t.ListLeafPanes()
		if len(leaves) > 0 {
			t.ActivePaneID = leaves[0]
		} else {
			t.ActivePaneID = ""
		}
	}

	return nil
}

// ComputeLayout calculates rectangular bounds for all nodes given terminal bounds and border gap.
func (t *Tree) ComputeLayout(bounds Rect, borderGap int) {
	if t.Root == nil {
		return
	}

	// If in monocle (zoom) mode, active pane takes full bounds
	if t.MonoclePaneID != "" {
		if node := t.FindNode(t.MonoclePaneID); node != nil {
			node.Rect = bounds
			return
		}
		t.MonoclePaneID = "" // Reset if node not found
	}

	computeNodeLayout(t.Root, bounds, borderGap)
}

func computeNodeLayout(node *Node, bounds Rect, borderGap int) {
	if node == nil {
		return
	}
	node.Rect = bounds

	if node.IsLeaf() {
		return
	}

	if node.Left == nil || node.Right == nil {
		return
	}

	ratio := node.Ratio
	if ratio <= 0.05 {
		ratio = 0.5
	}

	if node.Split == SplitVertical {
		availableWidth := bounds.W - borderGap
		if availableWidth < 2 {
			availableWidth = 2
		}
		leftW := int(float64(availableWidth) * ratio)
		rightW := bounds.W - leftW - borderGap

		leftRect := Rect{
			X: bounds.X,
			Y: bounds.Y,
			W: leftW,
			H: bounds.H,
		}
		rightRect := Rect{
			X: bounds.X + leftW + borderGap,
			Y: bounds.Y,
			W: rightW,
			H: bounds.H,
		}

		computeNodeLayout(node.Left, leftRect, borderGap)
		computeNodeLayout(node.Right, rightRect, borderGap)
	} else {
		availableHeight := bounds.H - borderGap
		if availableHeight < 2 {
			availableHeight = 2
		}
		topH := int(float64(availableHeight) * ratio)
		bottomH := bounds.H - topH - borderGap

		topRect := Rect{
			X: bounds.X,
			Y: bounds.Y,
			W: bounds.W,
			H: topH,
		}
		bottomRect := Rect{
			X: bounds.X,
			Y: bounds.Y + topH + borderGap,
			W: bounds.W,
			H: bottomH,
		}

		computeNodeLayout(node.Left, topRect, borderGap)
		computeNodeLayout(node.Right, bottomRect, borderGap)
	}
}

// FindNeighbor performs spatial geometric selection to locate the adjacent pane in dir.
func (t *Tree) FindNeighbor(currentPaneID string, dir NavDirection) string {
	current := t.FindNode(currentPaneID)
	if current == nil {
		return currentPaneID
	}

	cx, cy := current.Rect.Center()
	leaves := t.collectLeafNodes(t.Root)

	var bestCandidate string
	minDist := math.MaxFloat64

	for _, leaf := range leaves {
		if leaf.PaneID == currentPaneID {
			continue
		}

		lx, ly := leaf.Rect.Center()
		dx := lx - cx
		dy := ly - cy

		isCandidate := false
		switch dir {
		case NavLeft:
			isCandidate = dx < 0 && math.Abs(dx) >= math.Abs(dy)*0.5
		case NavRight:
			isCandidate = dx > 0 && math.Abs(dx) >= math.Abs(dy)*0.5
		case NavUp:
			isCandidate = dy < 0 && math.Abs(dy) >= math.Abs(dx)*0.5
		case NavDown:
			isCandidate = dy > 0 && math.Abs(dy) >= math.Abs(dx)*0.5
		}

		if isCandidate {
			dist := dx*dx + dy*dy
			if dist < minDist {
				minDist = dist
				bestCandidate = leaf.PaneID
			}
		}
	}

	if bestCandidate == "" {
		return currentPaneID
	}
	return bestCandidate
}

// Swap exchanges two panes in the tree hierarchy.
func (t *Tree) Swap(paneID1, paneID2 string) error {
	node1 := t.FindNode(paneID1)
	node2 := t.FindNode(paneID2)
	if node1 == nil || node2 == nil {
		return errors.New("one or both panes not found")
	}

	node1.PaneID, node2.PaneID = node2.PaneID, node1.PaneID
	return nil
}

// ToggleMonocle maximizes the pane or restores normal layout.
func (t *Tree) ToggleMonocle(paneID string) {
	if t.MonoclePaneID == paneID {
		t.MonoclePaneID = ""
	} else {
		t.MonoclePaneID = paneID
	}
}

// AdjustRatio modifies the split ratio of the parent of paneID.
func (t *Tree) AdjustRatio(paneID string, delta float64) {
	node := t.FindNode(paneID)
	if node == nil || node.Parent == nil {
		return
	}

	parent := node.Parent
	newRatio := parent.Ratio + delta
	if newRatio < 0.1 {
		newRatio = 0.1
	} else if newRatio > 0.9 {
		newRatio = 0.9
	}
	parent.Ratio = newRatio
}

// ListLeafPanes returns all leaf pane IDs in order of left-to-right tree traversal.
func (t *Tree) ListLeafPanes() []string {
	leaves := t.collectLeafNodes(t.Root)
	res := make([]string, len(leaves))
	for i, l := range leaves {
		res[i] = l.PaneID
	}
	return res
}

func (t *Tree) collectLeafNodes(node *Node) []*Node {
	if node == nil {
		return nil
	}
	if node.IsLeaf() {
		return []*Node{node}
	}
	res := t.collectLeafNodes(node.Left)
	res = append(res, t.collectLeafNodes(node.Right)...)
	return res
}
