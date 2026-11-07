package state

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/SujanPoudel1080/dajimux/internal/bsp"
)

func TestBSPNodeSerialization(t *testing.T) {
	root := &bsp.Node{
		ID:    "node-1",
		Split: bsp.SplitHorizontal,
		Ratio: 0.6,
		Left: &bsp.Node{
			ID:     "node-2",
			PaneID: "pane-1",
		},
		Right: &bsp.Node{
			ID:     "node-3",
			PaneID: "pane-2",
		},
	}
	root.Left.Parent = root
	root.Right.Parent = root

	sn := SerializeBSPNode(root)
	if sn == nil || sn.ID != "node-1" {
		t.Fatalf("expected serialized node root")
	}
	if sn.Split != bsp.SplitHorizontal || sn.Ratio != 0.6 {
		t.Errorf("unexpected split or ratio")
	}
	if sn.Left == nil || sn.Left.PaneID != "pane-1" {
		t.Errorf("unexpected left leaf")
	}
	if sn.Right == nil || sn.Right.PaneID != "pane-2" {
		t.Errorf("unexpected right leaf")
	}

	restored := DeserializeBSPNode(sn, nil)
	if restored == nil || restored.ID != "node-1" {
		t.Fatalf("failed to deserialize node")
	}
	if restored.Left == nil || restored.Left.Parent != restored || restored.Left.PaneID != "pane-1" {
		t.Errorf("deserialized left node mismatch")
	}
	if restored.Right == nil || restored.Right.Parent != restored || restored.Right.PaneID != "pane-2" {
		t.Errorf("deserialized right node mismatch")
	}
}

func TestSaveAndLoadStateFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "state.json")

	state := &SerializedState{
		Version: 1,
		SavedAt: time.Now(),
		Sessions: map[string]*SerializedSession{
			"dev": {
				Name:     "dev",
				ActiveWS: 1,
				Workspaces: map[int]*SerializedWorkspace{
					1: {
						ID:           1,
						Name:         "code",
						ActivePaneID: "pane-1",
						Tree: &SerializedBSPNode{
							ID:     "node-1",
							PaneID: "pane-1",
							Ratio:  0.5,
						},
					},
				},
				Panes: map[string]*SerializedPane{
					"pane-1": {
						ID:    "pane-1",
						Title: "editor",
						Cwd:   "/tmp",
					},
				},
			},
		},
	}

	if err := SaveStateToFile(path, state); err != nil {
		t.Fatalf("SaveStateToFile error: %v", err)
	}

	loaded, err := LoadStateFromFile(path)
	if err != nil {
		t.Fatalf("LoadStateFromFile error: %v", err)
	}

	if len(loaded.Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(loaded.Sessions))
	}
	dev := loaded.Sessions["dev"]
	if dev == nil || dev.Name != "dev" {
		t.Fatalf("session dev not found")
	}
	if len(dev.Workspaces) != 1 || dev.Panes["pane-1"].Cwd != "/tmp" {
		t.Errorf("data mismatch in loaded session: %+v", dev)
	}
}

func TestLoadNonExistentStateFileReturnsEmpty(t *testing.T) {
	loaded, err := LoadStateFromFile("/non/existent/path/state.json")
	if err != nil {
		t.Fatalf("expected nil error on missing state file, got %v", err)
	}
	if loaded == nil || len(loaded.Sessions) != 0 {
		t.Fatalf("expected empty sessions map, got %+v", loaded)
	}
}
