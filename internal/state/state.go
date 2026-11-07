package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/SujanPoudel1080/dajimux/internal/bsp"
)

// SerializedState represents the global serialized session store.
type SerializedState struct {
	Version  int                          `json:"version"`
	SavedAt  time.Time                    `json:"saved_at"`
	Sessions map[string]*SerializedSession `json:"sessions"`
}

// SerializedSession represents an active or persisted multiplexer session.
type SerializedSession struct {
	Name       string                         `json:"name"`
	ActiveWS   int                            `json:"active_ws"`
	Workspaces map[int]*SerializedWorkspace   `json:"workspaces"`
	Panes      map[string]*SerializedPane     `json:"panes"`
}

// SerializedWorkspace represents a virtual desktop workspace layout.
type SerializedWorkspace struct {
	ID           int                `json:"id"`
	Name         string             `json:"name"`
	ActivePaneID string             `json:"active_pane_id"`
	Tree         *SerializedBSPNode `json:"tree"`
}

// SerializedBSPNode represents a partition split or leaf pane in the BSP tree.
type SerializedBSPNode struct {
	ID     string             `json:"id"`
	Split  bsp.SplitDirection `json:"split"`
	Ratio  float64            `json:"ratio"`
	PaneID string             `json:"pane_id,omitempty"`
	Left   *SerializedBSPNode `json:"left,omitempty"`
	Right  *SerializedBSPNode `json:"right,omitempty"`
}

// SerializedPane represents metadata needed to reconstruct a pane's shell.
type SerializedPane struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Cwd        string `json:"cwd"`
	Shell      string `json:"shell,omitempty"`
	AgentState string `json:"agent_state,omitempty"`
	AgentBadge string `json:"agent_badge,omitempty"`
}

// DefaultStatePath returns ~/.local/share/dajimux/session_state.json.
func DefaultStatePath() string {
	if env := os.Getenv("DAJIMUX_STATE_PATH"); env != "" {
		return env
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "dajimux", "session_state.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "session_state.json"
	}
	return filepath.Join(home, ".local", "share", "dajimux", "session_state.json")
}

// SerializeBSPNode recursively converts a BSP tree node into its JSON-safe serializable form.
func SerializeBSPNode(n *bsp.Node) *SerializedBSPNode {
	if n == nil {
		return nil
	}
	sn := &SerializedBSPNode{
		ID:     n.ID,
		Split:  n.Split,
		Ratio:  n.Ratio,
		PaneID: n.PaneID,
	}
	if n.Left != nil {
		sn.Left = SerializeBSPNode(n.Left)
	}
	if n.Right != nil {
		sn.Right = SerializeBSPNode(n.Right)
	}
	return sn
}

// DeserializeBSPNode recursively reconstructs a BSP node tree from serialized data.
func DeserializeBSPNode(sn *SerializedBSPNode, parent *bsp.Node) *bsp.Node {
	if sn == nil {
		return nil
	}
	node := &bsp.Node{
		ID:     sn.ID,
		Parent: parent,
		Split:  sn.Split,
		Ratio:  sn.Ratio,
		PaneID: sn.PaneID,
	}
	if sn.Left != nil {
		node.Left = DeserializeBSPNode(sn.Left, node)
	}
	if sn.Right != nil {
		node.Right = DeserializeBSPNode(sn.Right, node)
	}
	return node
}

// SaveStateToFile writes the serialized state snapshot to disk atomically.
func SaveStateToFile(path string, state *SerializedState) error {
	if path == "" {
		path = DefaultStatePath()
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	tmpFile := fmt.Sprintf("%s.tmp.%d", path, time.Now().UnixNano())
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return fmt.Errorf("write tmp state file: %w", err)
	}

	if err := os.Rename(tmpFile, path); err != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("atomic rename state file: %w", err)
	}

	return nil
}

// LoadStateFromFile reads and unmarshals the serialized session state from disk.
func LoadStateFromFile(path string) (*SerializedState, error) {
	if path == "" {
		path = DefaultStatePath()
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &SerializedState{
				Version:  1,
				SavedAt:  time.Now(),
				Sessions: make(map[string]*SerializedSession),
			}, nil
		}
		return nil, fmt.Errorf("read state file: %w", err)
	}

	var state SerializedState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("unmarshal state: %w", err)
	}

	if state.Sessions == nil {
		state.Sessions = make(map[string]*SerializedSession)
	}

	return &state, nil
}
