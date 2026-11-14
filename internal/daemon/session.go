package daemon

import (
	"fmt"
	"sync"
	"time"

	"github.com/SujanPoudel1080/dajimux/internal/agent"
	"github.com/SujanPoudel1080/dajimux/internal/bsp"
	"github.com/SujanPoudel1080/dajimux/internal/config"
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
	Config      *config.Config
	paneCounter int
	isClosed    bool
	OnStateChange func(state protocol.LayoutStatePayload)
}

// NewSession initializes a session with workspace 1 and an initial pane using default configuration.
func NewSession(name string, cols, rows int, onStateChange func(protocol.LayoutStatePayload)) (*Session, error) {
	return NewSessionWithConfig(name, cols, rows, nil, onStateChange)
}

// NewSessionWithConfig initializes a session with user configuration.
func NewSessionWithConfig(name string, cols, rows int, cfg *config.Config, onStateChange func(protocol.LayoutStatePayload)) (*Session, error) {
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	if cfg == nil {
		cfg = config.DefaultConfig()
	}

	s := &Session{
		Name:          name,
		Workspaces:    make(map[int]*Workspace),
		ActiveWS:      1,
		Panes:         make(map[string]*Pane),
		Mode:          protocol.ModeInsert,
		Cols:          cols,
		Rows:          rows,
		Config:        cfg,
		paneCounter:   1,
		OnStateChange: onStateChange,
	}

	s.Broker = agent.NewBroker(func(status *agent.PaneStatus) {
		s.mu.Lock()
		if pane, ok := s.Panes[status.PaneID]; ok {
			pane.mu.Lock()
			pane.AgentState = status.State
			pane.AgentBadge = status.Badge
			pane.mu.Unlock()
		}
		s.mu.Unlock()
		s.NotifyState()
	})

	// Create initial pane
	initialPaneID := s.nextPaneID()
	initialPane := s.createPane(initialPaneID, "shell")
	s.Panes[initialPaneID] = initialPane

	ws := NewWorkspace(1, "main", initialPaneID)
	ws.Layout(cols, rows)
	s.Workspaces[1] = ws

	// Start initial shell
	env := []string{
		fmt.Sprintf("DAJIMUX_SESSION=%s", s.Name),
		fmt.Sprintf("DAJIMUX_WORKSPACE=%d", 1),
	}
	if err := initialPane.Start(s.defaultShell(), ws.Bounds.W, ws.Bounds.H, env); err != nil {
		return nil, fmt.Errorf("start initial pane: %w", err)
	}

	return s, nil
}

func (s *Session) createPane(id, title string) *Pane {
	maxLines := 2000
	if s.Config != nil && s.Config.General.ScrollbackLines > 0 {
		maxLines = s.Config.General.ScrollbackLines
	}
	p := NewPaneWithConfig(id, title, maxLines)
	p.SetOnExit(func(pid string) {
		_ = s.ClosePaneByID(pid)
	})
	p.SetOnTitleChange(func(pid, newTitle string) {
		go s.NotifyState()
	})
	return p
}

func (s *Session) defaultShell() string {
	if s.Config != nil && s.Config.General.DefaultShell != "" {
		return s.Config.General.DefaultShell
	}
	return ""
}

func (s *Session) nextPaneID() string {
	id := fmt.Sprintf("pane-%d", s.paneCounter)
	s.paneCounter++
	return id
}

// ActiveWorkspace returns the currently selected workspace.
func (s *Session) ActiveWorkspace() *Workspace {
	s.mu.RLock()
	defer s.mu.RUnlock()
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

// VisiblePanes returns all leaf panes currently visible in the active workspace.
// If monocle mode is active, it returns only the focused/monocle pane.
func (s *Session) VisiblePanes() []*Pane {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ws := s.Workspaces[s.ActiveWS]
	if ws == nil {
		return nil
	}

	if ws.Tree != nil && ws.Tree.MonoclePaneID != "" {
		if p, ok := s.Panes[ws.Tree.MonoclePaneID]; ok && p != nil {
			return []*Pane{p}
		}
	}

	if ws.Tree == nil {
		return nil
	}

	leafIDs := ws.Tree.ListLeafPanes()
	panes := make([]*Pane, 0, len(leafIDs))
	for _, pid := range leafIDs {
		if p, ok := s.Panes[pid]; ok && p != nil {
			panes = append(panes, p)
		}
	}
	return panes
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
	newPane := s.createPane(newID, "shell")
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

	hasBorders := len(ws.Tree.ListLeafPanes()) > 1 && ws.Tree.MonoclePaneID == ""
	w, h := 40, 12
	if node != nil {
		w, h = node.Rect.W, node.Rect.H
		if hasBorders {
			w -= 2
			h -= 2
		}
	}
	if w < 2 {
		w = 2
	}
	if h < 2 {
		h = 2
	}

	if err := newPane.Start(s.defaultShell(), w, h, env); err != nil {
		_ = ws.Tree.Close(newID)
		delete(s.Panes, newID)
		return nil, fmt.Errorf("failed to spawn pane process: %w", err)
	}

	// Update bounds of existing panes
	s.syncPaneSizes(ws)

	go s.NotifyState()
	return newPane, nil
}

// SplitActiveAuto automatically splits the active pane vertically if wide, or horizontally if tall.
func (s *Session) SplitActiveAuto() (*Pane, error) {
	s.mu.RLock()
	ws := s.Workspaces[s.ActiveWS]
	if ws == nil {
		s.mu.RUnlock()
		return nil, fmt.Errorf("no active workspace")
	}
	targetID := ws.ActivePaneID()
	dir := bsp.SplitHorizontal
	if ws.Tree != nil {
		if node := ws.Tree.FindNode(targetID); node != nil {
			if node.Rect.W > (node.Rect.H * 2) {
				dir = bsp.SplitVertical
			}
		}
	}
	s.mu.RUnlock()

	return s.SplitActive(dir)
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
	newPane := s.createPane(newID, "shell")
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

	hasBorders := len(ws.Tree.ListLeafPanes()) > 1 && ws.Tree.MonoclePaneID == ""
	w, h := 40, 12
	if node != nil {
		w, h = node.Rect.W, node.Rect.H
		if hasBorders {
			w -= 2
			h -= 2
		}
	}
	if w < 2 {
		w = 2
	}
	if h < 2 {
		h = 2
	}

	if err := newPane.StartWithCwd(s.defaultShell(), w, h, env, cwd); err != nil {
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
	s.mu.RLock()
	ws := s.Workspaces[s.ActiveWS]
	if ws == nil {
		s.mu.RUnlock()
		return fmt.Errorf("no active workspace")
	}
	activeID := ws.ActivePaneID()
	s.mu.RUnlock()

	if activeID == "" {
		return nil
	}
	return s.ClosePaneByID(activeID)
}

// GetPane returns the pane with paneID under read lock, or nil if not found.
func (s *Session) GetPane(paneID string) *Pane {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Panes[paneID]
}

// Close terminates all panes in the session and marks it as closed.
func (s *Session) Close() {
	s.mu.Lock()
	if s.isClosed {
		s.mu.Unlock()
		return
	}
	s.isClosed = true
	panes := make([]*Pane, 0, len(s.Panes))
	for _, p := range s.Panes {
		panes = append(panes, p)
	}
	s.Panes = make(map[string]*Pane)
	s.mu.Unlock()

	for _, p := range panes {
		_ = p.Close()
	}
}

// ClosePaneByID terminates and removes a specific pane by its ID across any workspace.
func (s *Session) ClosePaneByID(paneID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isClosed {
		return nil
	}

	pane, ok := s.Panes[paneID]
	if !ok {
		return nil
	}

	delete(s.Panes, paneID)
	_ = pane.Close()
	s.Broker.RemovePane(paneID)

	// Locate workspace holding this pane
	var targetWS *Workspace
	var targetWSID int
	for wsID, ws := range s.Workspaces {
		if ws.Tree != nil && ws.Tree.FindNode(paneID) != nil {
			targetWS = ws
			targetWSID = wsID
			break
		}
	}

	if targetWS == nil {
		return nil
	}

	if err := targetWS.Tree.Close(paneID); err != nil {
		return err
	}

	if targetWS.Tree.Root == nil {
		// Workspace empty; delete workspace unless it's ws 1
		if targetWSID != 1 {
			delete(s.Workspaces, targetWSID)
			if s.ActiveWS == targetWSID {
				s.ActiveWS = 1
			}
		} else if !s.isClosed {
			// Respawn single initial pane in ws 1
			newID := s.nextPaneID()
			newPane := s.createPane(newID, "shell")
			s.Panes[newID] = newPane
			targetWS.Tree = bsp.NewTree(newID)
			env := []string{
				fmt.Sprintf("DAJIMUX_SESSION=%s", s.Name),
				fmt.Sprintf("DAJIMUX_WORKSPACE=%d", 1),
			}
			_ = newPane.Start(s.defaultShell(), s.Cols, s.Rows-2, env)
		}
	}

	targetWS.Layout(s.Cols, s.Rows)
	s.syncPaneSizes(targetWS)

	if !s.isClosed {
		go s.NotifyState()
	}
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
		replacementPane := s.createPane(newID, "shell")
		s.Panes[newID] = replacementPane
		env := []string{
			fmt.Sprintf("DAJIMUX_SESSION=%s", s.Name),
			fmt.Sprintf("DAJIMUX_WORKSPACE=%d", s.ActiveWS),
		}
		_ = replacementPane.Start(s.defaultShell(), srcWS.Bounds.W, srcWS.Bounds.H, env)
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
		newPane := s.createPane(newID, "shell")
		s.Panes[newID] = newPane

		ws = NewWorkspace(num, fmt.Sprintf("ws-%d", num), newID)
		ws.Layout(s.Cols, s.Rows)
		s.Workspaces[num] = ws

		env := []string{
			fmt.Sprintf("DAJIMUX_SESSION=%s", s.Name),
			fmt.Sprintf("DAJIMUX_WORKSPACE=%d", num),
		}
		_ = newPane.Start(s.defaultShell(), ws.Bounds.W, ws.Bounds.H, env)
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
	hasBorders := len(ws.Tree.ListLeafPanes()) > 1 && ws.Tree.MonoclePaneID == ""
	for _, paneID := range ws.Tree.ListLeafPanes() {
		node := ws.Tree.FindNode(paneID)
		pane := s.Panes[paneID]
		if node != nil && pane != nil {
			cols := node.Rect.W
			rows := node.Rect.H
			if hasBorders {
				cols -= 2
				rows -= 2
			}
			if cols < 2 {
				cols = 2
			}
			if rows < 2 {
				rows = 2
			}
			_ = pane.Resize(cols, rows)
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

// RenameWorkspace changes the friendly name of workspace wsID.
func (s *Session) RenameWorkspace(wsID int, newName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ws, ok := s.Workspaces[wsID]; ok && newName != "" {
		ws.Rename(newName)
	}
	go s.NotifyState()
}

// CycleLayout advances the layout preset on the active workspace.
func (s *Session) CycleLayout() {
	s.mu.Lock()
	defer s.mu.Unlock()
	ws := s.Workspaces[s.ActiveWS]
	if ws != nil && ws.Tree != nil {
		ws.Tree.CycleLayout()
		ws.Layout(s.Cols, s.Rows)
		s.syncPaneSizes(ws)
	}
	go s.NotifyState()
}

// SetLayout updates the layout preset for the active workspace.
func (s *Session) SetLayout(lt bsp.LayoutType) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ws := s.Workspaces[s.ActiveWS]
	if ws != nil && ws.Tree != nil {
		ws.Tree.SetLayoutType(lt)
		ws.Layout(s.Cols, s.Rows)
		s.syncPaneSizes(ws)
	}
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
			pane.mu.RLock()
			pTitle := pane.Title
			pState := pane.AgentState
			pBadge := pane.AgentBadge
			pane.mu.RUnlock()
			info := protocol.PaneInfo{
				ID:         pid,
				Title:      pTitle,
				X:          node.Rect.X,
				Y:          node.Rect.Y,
				W:          node.Rect.W,
				H:          node.Rect.H,
				IsActive:   pid == ws.ActivePaneID(),
				AgentState: pState,
				AgentBadge: pBadge,
			}
			panes = append(panes, info)
		}
	}

	workspacesMeta := make([]protocol.WorkspaceMeta, 0, len(s.Workspaces))
	for i := 1; i <= 9; i++ {
		if w, ok := s.Workspaces[i]; ok {
			workspacesMeta = append(workspacesMeta, protocol.WorkspaceMeta{
				ID:   i,
				Name: w.Name,
			})
		}
	}

	layoutTypeStr := "bsp"
	if ws != nil && ws.Tree != nil && ws.Tree.LayoutType != "" {
		layoutTypeStr = string(ws.Tree.LayoutType)
	}

	return protocol.LayoutStatePayload{
		SessionName:     s.Name,
		ActiveWorkspace: s.ActiveWS,
		TotalWorkspaces: len(s.Workspaces),
		Workspaces:      workspacesMeta,
		LayoutType:      layoutTypeStr,
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
			LayoutType:   string(ws.Tree.LayoutType),
			ActivePaneID: ws.Tree.ActivePaneID,
			Tree:         state.SerializeBSPNode(ws.Tree.Root),
		}
		ws.mu.RUnlock()
		ss.Workspaces[id] = sws
	}

	return ss
}

// RestoreSessionFromSerialized reconstructs a live Session from saved state.
// RestoreSessionFromSerialized reconstructs a session from saved state and restarts shells.
func RestoreSessionFromSerialized(ss *state.SerializedSession, cols, rows int, onStateChange func(protocol.LayoutStatePayload)) (*Session, error) {
	return RestoreSessionFromSerializedWithConfig(ss, cols, rows, nil, onStateChange)
}

// RestoreSessionFromSerializedWithConfig reconstructs a session from saved state and configuration.
func RestoreSessionFromSerializedWithConfig(ss *state.SerializedSession, cols, rows int, cfg *config.Config, onStateChange func(protocol.LayoutStatePayload)) (*Session, error) {
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	if cfg == nil {
		cfg = config.DefaultConfig()
	}

	s := &Session{
		Name:          ss.Name,
		Workspaces:    make(map[int]*Workspace),
		ActiveWS:      ss.ActiveWS,
		Panes:         make(map[string]*Pane),
		Mode:          protocol.ModeInsert,
		Cols:          cols,
		Rows:          rows,
		Config:        cfg,
		paneCounter:   1,
		OnStateChange: onStateChange,
	}

	s.Broker = agent.NewBroker(func(status *agent.PaneStatus) {
		s.mu.Lock()
		if pane, ok := s.Panes[status.PaneID]; ok {
			pane.mu.Lock()
			pane.AgentState = status.State
			pane.AgentBadge = status.Badge
			pane.mu.Unlock()
		}
		s.mu.Unlock()
		s.NotifyState()
	})

	// Recreate panes and launch processes in their respective CWDs
	for id, sp := range ss.Panes {
		pane := s.createPane(id, sp.Title)
		pane.Cwd = sp.Cwd
		pane.AgentState = sp.AgentState
		pane.AgentBadge = sp.AgentBadge
		s.Panes[id] = pane

		env := []string{
			fmt.Sprintf("DAJIMUX_SESSION=%s", s.Name),
		}
		_ = pane.StartWithCwd(s.defaultShell(), cols, rows-2, env, sp.Cwd)
	}

	// Recreate workspaces and BSP trees
	for id, sws := range ss.Workspaces {
		lt := bsp.LayoutBSP
		if sws.LayoutType != "" {
			lt = bsp.LayoutType(sws.LayoutType)
		}
		ws := &Workspace{
			ID:   sws.ID,
			Name: sws.Name,
			Tree: &bsp.Tree{
				Root:          state.DeserializeBSPNode(sws.Tree, nil),
				ActivePaneID:  sws.ActivePaneID,
				LayoutType:    lt,
				MasterRatio:   0.60,
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
		initialPane := s.createPane(initialPaneID, "shell")
		s.Panes[initialPaneID] = initialPane
		ws := NewWorkspace(1, "main", initialPaneID)
		ws.Layout(cols, rows)
		s.Workspaces[1] = ws
		_ = initialPane.Start(s.defaultShell(), ws.Bounds.W, ws.Bounds.H, nil)
	}

	s.paneCounter = len(s.Panes) + 1
	return s, nil
}
