package daemon

import (
	"fmt"
	"sync"
	"time"

	"github.com/SujanPoudel1080/dajimux/internal/agent"
	"github.com/SujanPoudel1080/dajimux/internal/bsp"
	"github.com/SujanPoudel1080/dajimux/internal/protocol"
	"github.com/SujanPoudel1080/dajimux/internal/state"
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

// SpawnPane dynamically splits the active pane and launches a shell with optional CWD and initial command.
func (s *Session) SpawnPane(dir bsp.SplitDirection, cwd string, initialCmd string) (*Pane, error) {
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

	if err := newPane.StartWithCwd("", w, h, env, cwd); err != nil {
		_ = ws.Tree.Close(newID)
		delete(s.Panes, newID)
		return nil, fmt.Errorf("failed to spawn pane process: %w", err)
	}

	ws.SetActivePane(newID)
	s.syncPaneSizes(ws)

	if initialCmd != "" {
		go func() {
			time.Sleep(100 * time.Millisecond)
			_, _ = newPane.Write([]byte(initialCmd + "\n"))
		}()
	}

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

// FocusPane focuses the pane with paneID in the active workspace.
func (s *Session) FocusPane(paneID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ws := s.Workspaces[s.ActiveWS]
	if ws == nil {
		return
	}

	node := ws.Tree.FindNode(paneID)
	if node != nil && ws.ActivePaneID() != paneID {
		ws.SetActivePane(paneID)
		go s.NotifyState()
	}
}

// SwapActiveWithNext swaps the currently focused pane with the next pane in the workspace tree.
func (s *Session) SwapActiveWithNext() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ws := s.Workspaces[s.ActiveWS]
	if ws == nil {
		return fmt.Errorf("no active workspace")
	}

	if err := ws.Tree.SwapNext(); err != nil {
		return err
	}

	ws.Layout(s.Cols, s.Rows)
	s.syncPaneSizes(ws)
	go s.NotifyState()
	return nil
}

// SwapActive swaps the currently focused pane with its spatial neighbor in direction dir.
func (s *Session) SwapActive(dir bsp.NavDirection) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ws := s.Workspaces[s.ActiveWS]
	if ws == nil {
		return fmt.Errorf("no active workspace")
	}

	if err := ws.Tree.SwapDirection(dir); err != nil {
		return err
	}

	ws.Layout(s.Cols, s.Rows)
	s.syncPaneSizes(ws)
	go s.NotifyState()
	return nil
}

// MoveActiveToWorkspace moves the currently focused pane to targetWS (1..9).
func (s *Session) MoveActiveToWorkspace(targetWS int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if targetWS < 1 || targetWS > 9 {
		return fmt.Errorf("invalid workspace %d", targetWS)
	}

	if targetWS == s.ActiveWS {
		return nil
	}

	srcWS := s.Workspaces[s.ActiveWS]
	if srcWS == nil {
		return fmt.Errorf("no active workspace")
	}

	activePaneID := srcWS.ActivePaneID()
	if activePaneID == "" {
		return fmt.Errorf("no active pane to move")
	}

	_, exists := s.Panes[activePaneID]
	if !exists {
		return fmt.Errorf("pane %s not found", activePaneID)
	}

	// Detach from current workspace
	srcLeaves := srcWS.Tree.ListLeafPanes()
	if len(srcLeaves) <= 1 {
		// Spawn replacement shell in source workspace so it is not left empty
		newID := s.nextPaneID()
		replacementPane := NewPane(newID, "shell")
		s.Panes[newID] = replacementPane
		env := []string{
			fmt.Sprintf("DAJIMUX_SESSION=%s", s.Name),
			fmt.Sprintf("DAJIMUX_WORKSPACE=%d", s.ActiveWS),
		}
		_ = replacementPane.Start("", srcWS.Bounds.W, srcWS.Bounds.H, env)
		srcWS.Tree = bsp.NewTree(newID)
	} else {
		_ = srcWS.Tree.Close(activePaneID)
	}

	srcWS.Layout(s.Cols, s.Rows)
	s.syncPaneSizes(srcWS)

	// Attach to target workspace
	tgtWS, tgtExists := s.Workspaces[targetWS]
	if !tgtExists {
		tgtWS = NewWorkspace(targetWS, fmt.Sprintf("ws-%d", targetWS), activePaneID)
		tgtWS.Layout(s.Cols, s.Rows)
		s.Workspaces[targetWS] = tgtWS
	} else {
		_, _ = tgtWS.Tree.InsertPane(activePaneID)
		tgtWS.SetActivePane(activePaneID)
		tgtWS.Layout(s.Cols, s.Rows)
	}
	s.syncPaneSizes(tgtWS)

	s.ActiveWS = targetWS
	go s.NotifyState()
	return nil
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

// ToSerialized converts the live session state into a serializable structure.
func (s *Session) ToSerialized() *state.SerializedSession {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ss := &state.SerializedSession{
		Name:       s.Name,
		ActiveWS:   s.ActiveWS,
		Workspaces: make(map[int]*state.SerializedWorkspace),
		Panes:      make(map[string]*state.SerializedPane),
	}

	for id, p := range s.Panes {
		p.mu.RLock()
		ss.Panes[id] = &state.SerializedPane{
			ID:         p.ID,
			Title:      p.Title,
			Cwd:        p.Cwd,
			AgentState: p.AgentState,
			AgentBadge: p.AgentBadge,
		}
		p.mu.RUnlock()
	}

	for id, ws := range s.Workspaces {
		ws.mu.RLock()
		sws := &state.SerializedWorkspace{
			ID:           ws.ID,
			Name:         ws.Name,
			ActivePaneID: ws.Tree.ActivePaneID,
			Tree:         state.SerializeBSPNode(ws.Tree.Root),
		}
		ws.mu.RUnlock()
		ss.Workspaces[id] = sws
	}

	return ss
}

// RestoreSessionFromSerialized reconstructs a live Session from saved state.
func RestoreSessionFromSerialized(ss *state.SerializedSession, cols, rows int, onStateChange func(protocol.LayoutStatePayload)) (*Session, error) {
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}

	s := &Session{
		Name:          ss.Name,
		Workspaces:    make(map[int]*Workspace),
		ActiveWS:      ss.ActiveWS,
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

	// Recreate panes and launch processes in their respective CWDs
	for id, sp := range ss.Panes {
		pane := NewPane(id, sp.Title)
		pane.Cwd = sp.Cwd
		pane.AgentState = sp.AgentState
		pane.AgentBadge = sp.AgentBadge
		s.Panes[id] = pane

		env := []string{
			fmt.Sprintf("DAJIMUX_SESSION=%s", s.Name),
		}
		_ = pane.StartWithCwd("", cols, rows-2, env, sp.Cwd)
	}

	// Recreate workspaces and BSP trees
	for id, sws := range ss.Workspaces {
		ws := &Workspace{
			ID:   sws.ID,
			Name: sws.Name,
			Tree: &bsp.Tree{
				Root:          state.DeserializeBSPNode(sws.Tree, nil),
				ActivePaneID:  sws.ActivePaneID,
				MonoclePaneID: "",
			},
		}
		ws.Layout(cols, rows)
		s.Workspaces[id] = ws
		s.syncPaneSizes(ws)
	}

	if s.ActiveWS == 0 || s.Workspaces[s.ActiveWS] == nil {
		s.ActiveWS = 1
	}

	// Ensure there is at least one workspace
	if len(s.Workspaces) == 0 {
		initialPaneID := s.nextPaneID()
		initialPane := NewPane(initialPaneID, "shell")
		s.Panes[initialPaneID] = initialPane
		ws := NewWorkspace(1, "main", initialPaneID)
		ws.Layout(cols, rows)
		s.Workspaces[1] = ws
		_ = initialPane.Start("", ws.Bounds.W, ws.Bounds.H, nil)
	}

	s.paneCounter = len(s.Panes) + 1
	return s, nil
}
