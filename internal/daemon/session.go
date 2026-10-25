package daemon

import (
	"fmt"
	"sync"

	"github.com/SujanPoudel1080/dajimux/internal/agent"
	"github.com/SujanPoudel1080/dajimux/internal/bsp"
	"github.com/SujanPoudel1080/dajimux/internal/protocol"
)

// Session manages workspaces, panes, and agent telemetry for a named session.
type Session struct {
	mu          sync.RWMutex
	Name        string
	Workspaces  map[int]*Workspace
	ActiveWS    int
	Panes       map[string]*Pane
	Broker      *agent.Broker
	Mode        protocol.Mode
	Cols        int
	Rows        int
	paneCounter int
	OnStateChange func(state protocol.LayoutStatePayload)
}

// NewSession initializes a session with workspace 1 and an initial pane.
func NewSession(name string, cols, rows int, onStateChange func(protocol.LayoutStatePayload)) (*Session, error) {
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}

	s := &Session{
		Name:          name,
		Workspaces:    make(map[int]*Workspace),
		ActiveWS:      1,
		Panes:         make(map[string]*Pane),
		Mode:          protocol.ModeInsert,
		Cols:          cols,
		Rows:          rows,
		paneCounter:   1,
		OnStateChange: onStateChange,
	}

	s.Broker = agent.NewBroker(func(status *agent.PaneStatus) {
		s.mu.Lock()
		if pane, ok := s.Panes[status.PaneID]; ok {
			pane.AgentState = status.State
			pane.AgentBadge = status.Badge
		}
		s.mu.Unlock()
		s.NotifyState()
	})

	// Create initial pane
	initialPaneID := s.nextPaneID()
	initialPane := NewPane(initialPaneID, "shell")
	s.Panes[initialPaneID] = initialPane

	ws := NewWorkspace(1, "main", initialPaneID)
	ws.Layout(cols, rows)
	s.Workspaces[1] = ws

	// Start initial shell
	env := []string{
		fmt.Sprintf("DAJIMUX_SESSION=%s", s.Name),
		fmt.Sprintf("DAJIMUX_WORKSPACE=%d", 1),
	}
	if err := initialPane.Start("", ws.Bounds.W, ws.Bounds.H, env); err != nil {
		return nil, fmt.Errorf("start initial pane: %w", err)
	}

	return s, nil
}

func (s *Session) nextPaneID() string {
	id := fmt.Sprintf("pane-%d", s.paneCounter)
	s.paneCounter++
	return id
}

// ActiveWorkspace returns the currently selected workspace.
func (s *Session) ActiveWorkspace() *Workspace {
	return s.Workspaces[s.ActiveWS]
}

// ActivePane returns the currently focused pane.
func (s *Session) ActivePane() *Pane {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ws := s.Workspaces[s.ActiveWS]
	if ws == nil {
		return nil
	}
	activeID := ws.ActivePaneID()
	return s.Panes[activeID]
}

// SplitActive splits the currently focused pane horizontally or vertically.
func (s *Session) SplitActive(dir bsp.SplitDirection) (*Pane, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ws := s.Workspaces[s.ActiveWS]
	if ws == nil {
		return nil, fmt.Errorf("no active workspace")
	}

	targetPaneID := ws.ActivePaneID()
	newID := s.nextPaneID()
	newPane := NewPane(newID, "shell")
	s.Panes[newID] = newPane

	_, err := ws.Tree.Split(targetPaneID, newID, dir, 0.5)
	if err != nil {
		delete(s.Panes, newID)
		return nil, err
	}

	ws.Layout(s.Cols, s.Rows)

	node := ws.Tree.FindNode(newID)
	env := []string{
		fmt.Sprintf("DAJIMUX_SESSION=%s", s.Name),
		fmt.Sprintf("DAJIMUX_WORKSPACE=%d", s.ActiveWS),
	}

	w, h := 40, 12
	if node != nil {
		w, h = node.Rect.W, node.Rect.H
	}

	if err := newPane.Start("", w, h, env); err != nil {
		_ = ws.Tree.Close(newID)
		delete(s.Panes, newID)
		return nil, fmt.Errorf("failed to spawn pane process: %w", err)
	}

	// Update bounds of existing panes
	s.syncPaneSizes(ws)

	go s.NotifyState()
	return newPane, nil
}

// CloseActive closes the focused pane and rebalances the tree.
func (s *Session) CloseActive() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ws := s.Workspaces[s.ActiveWS]
	if ws == nil {
		return fmt.Errorf("no active workspace")
	}

	activeID := ws.ActivePaneID()
	if activeID == "" {
		return nil
	}

	pane := s.Panes[activeID]
	if pane != nil {
		pane.Close()
		delete(s.Panes, activeID)
		s.Broker.RemovePane(activeID)
	}

	if err := ws.Tree.Close(activeID); err != nil {
		return err
	}

	if ws.Tree.Root == nil {
		// Workspace empty; delete workspace unless it's ws 1
		if s.ActiveWS != 1 {
			delete(s.Workspaces, s.ActiveWS)
			s.ActiveWS = 1
		} else {
			// Respawn single initial pane in ws 1
			newID := s.nextPaneID()
			newPane := NewPane(newID, "shell")
			s.Panes[newID] = newPane
			ws.Tree = bsp.NewTree(newID)
			_ = newPane.Start("", s.Cols, s.Rows-2, nil)
		}
	}

	ws.Layout(s.Cols, s.Rows)
	s.syncPaneSizes(ws)

	go s.NotifyState()
	return nil
}

// Navigate moves focus to adjacent pane in direction dir.
func (s *Session) Navigate(dir bsp.NavDirection) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ws := s.Workspaces[s.ActiveWS]
	if ws == nil {
		return
	}

	curr := ws.ActivePaneID()
	target := ws.Tree.FindNeighbor(curr, dir)
	if target != "" && target != curr {
		ws.SetActivePane(target)
		go s.NotifyState()
	}
}

// SwitchWorkspace switches to workspace num (1..9), creating it if necessary.
func (s *Session) SwitchWorkspace(num int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if num < 1 || num > 9 {
		return
	}

	ws, exists := s.Workspaces[num]
	if !exists {
		newID := s.nextPaneID()
		newPane := NewPane(newID, "shell")
		s.Panes[newID] = newPane

		ws = NewWorkspace(num, fmt.Sprintf("ws-%d", num), newID)
		ws.Layout(s.Cols, s.Rows)
		s.Workspaces[num] = ws

		env := []string{
			fmt.Sprintf("DAJIMUX_SESSION=%s", s.Name),
			fmt.Sprintf("DAJIMUX_WORKSPACE=%d", num),
		}
		_ = newPane.Start("", ws.Bounds.W, ws.Bounds.H, env)
	}

	s.ActiveWS = num
	ws.Layout(s.Cols, s.Rows)
	s.syncPaneSizes(ws)

	go s.NotifyState()
}

// ResizeTerminal handles terminal resize events (SIGWINCH).
func (s *Session) ResizeTerminal(cols, rows int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Cols = cols
	s.Rows = rows

	for _, ws := range s.Workspaces {
		ws.Layout(cols, rows)
		s.syncPaneSizes(ws)
	}

	go s.NotifyState()
}

// AdjustSplit adjusts the split ratio of the active pane.
func (s *Session) AdjustSplit(delta float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ws := s.Workspaces[s.ActiveWS]
	if ws == nil {
		return
	}

	ws.Tree.AdjustRatio(ws.ActivePaneID(), delta)
	ws.Layout(s.Cols, s.Rows)
	s.syncPaneSizes(ws)

	go s.NotifyState()
}

// ToggleMonocle toggles fullscreen zoom for the active pane.
func (s *Session) ToggleMonocle() {
	s.mu.Lock()
	defer s.mu.Unlock()

	ws := s.Workspaces[s.ActiveWS]
	if ws == nil {
		return
	}

	ws.Tree.ToggleMonocle(ws.ActivePaneID())
	ws.Layout(s.Cols, s.Rows)
	s.syncPaneSizes(ws)

	go s.NotifyState()
}

func (s *Session) syncPaneSizes(ws *Workspace) {
	for _, paneID := range ws.Tree.ListLeafPanes() {
		node := ws.Tree.FindNode(paneID)
		pane := s.Panes[paneID]
		if node != nil && pane != nil {
			_ = pane.Resize(node.Rect.W, node.Rect.H)
		}
	}
}

// SetMode updates the modal interface state.
func (s *Session) SetMode(m protocol.Mode) {
	s.mu.Lock()
	s.Mode = m
	s.mu.Unlock()
	go s.NotifyState()
}

// GetLayoutState generates the current state payload for clients.
func (s *Session) GetLayoutState() protocol.LayoutStatePayload {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ws := s.Workspaces[s.ActiveWS]
	if ws == nil {
		return protocol.LayoutStatePayload{
			SessionName: s.Name,
			Mode:        s.Mode,
		}
	}

	panes := make([]protocol.PaneInfo, 0, len(s.Panes))
	for _, pid := range ws.Tree.ListLeafPanes() {
		node := ws.Tree.FindNode(pid)
		pane := s.Panes[pid]
		if node != nil && pane != nil {
			info := protocol.PaneInfo{
				ID:         pid,
				Title:      pane.Title,
				X:          node.Rect.X,
				Y:          node.Rect.Y,
				W:          node.Rect.W,
				H:          node.Rect.H,
				IsActive:   pid == ws.ActivePaneID(),
				AgentState: pane.AgentState,
				AgentBadge: pane.AgentBadge,
			}
			panes = append(panes, info)
		}
	}

	return protocol.LayoutStatePayload{
		SessionName:     s.Name,
		ActiveWorkspace: s.ActiveWS,
		TotalWorkspaces: len(s.Workspaces),
		ActivePaneID:    ws.ActivePaneID(),
		Mode:            s.Mode,
		Panes:           panes,
		IsMonocle:       ws.Tree.MonoclePaneID != "",
	}
}

// NotifyState broadcasts the layout state to registered listeners.
func (s *Session) NotifyState() {
	if s.OnStateChange != nil {
		s.OnStateChange(s.GetLayoutState())
	}
}
