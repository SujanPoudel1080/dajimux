package daemon

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/SujanPoudel1080/dajimux/internal/bsp"
	"github.com/SujanPoudel1080/dajimux/internal/config"
	"github.com/SujanPoudel1080/dajimux/internal/protocol"
	"github.com/SujanPoudel1080/dajimux/internal/state"
)

// Daemon coordinates sessions, clients, and IPC sockets.
type Daemon struct {
	mu             sync.RWMutex
	SocketPath     string
	listener       net.Listener
	Sessions       map[string]*Session
	DefaultSession string
	Config         *config.Config
	clients        map[net.Conn]*clientEntry
	isClosing      bool
}

type clientEntry struct {
	session *Session
	writer  *protocol.EnvelopeWriter
	cols    int
	rows    int
}

// DefaultSocketPath returns the standard socket location for the current user.
func DefaultSocketPath() string {
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		dir := filepath.Join(xdg, "dajimux")
		_ = os.MkdirAll(dir, 0700)
		return filepath.Join(dir, "daemon.sock")
	}
	uid := os.Getuid()
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("dajimux-%d", uid))
	_ = os.MkdirAll(dir, 0700)
	return filepath.Join(dir, "daemon.sock")
}

// NewDaemon initializes a daemon server with default configuration.
func NewDaemon(socketPath string) *Daemon {
	return NewDaemonWithConfig(socketPath, nil)
}

// NewDaemonWithConfig initializes a daemon server with user configuration.
func NewDaemonWithConfig(socketPath string, cfg *config.Config) *Daemon {
	if socketPath == "" {
		socketPath = DefaultSocketPath()
	}
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	return &Daemon{
		SocketPath:     socketPath,
		Sessions:       make(map[string]*Session),
		DefaultSession: "main",
		Config:         cfg,
		clients:        make(map[net.Conn]*clientEntry),
	}
}

// Start opens the Unix domain socket and listens for client connections.
func (d *Daemon) Start() error {
	// Clean up stale socket file if it exists
	if _, err := os.Stat(d.SocketPath); err == nil {
		// Test if active daemon is listening
		conn, err := net.Dial("unix", d.SocketPath)
		if err == nil {
			conn.Close()
			return fmt.Errorf("daemon is already running on %s", d.SocketPath)
		}
		_ = os.Remove(d.SocketPath)
	}

	listener, err := net.Listen("unix", d.SocketPath)
	if err != nil {
		return fmt.Errorf("listen on unix socket %s: %w", d.SocketPath, err)
	}
	_ = os.Chmod(d.SocketPath, 0600)
	d.listener = listener

	// Ensure default session exists
	if _, err := d.GetOrCreateSession(d.DefaultSession, 80, 24); err != nil {
		return fmt.Errorf("create initial session: %w", err)
	}

	return nil
}

// AcceptLoop continuously accepts and handles incoming client connections.
func (d *Daemon) AcceptLoop() error {
	for {
		conn, err := d.listener.Accept()
		if err != nil {
			d.mu.RLock()
			closing := d.isClosing
			d.mu.RUnlock()
			if closing {
				return nil
			}
			return err
		}

		go d.handleConnection(conn)
	}
}

// GetOrCreateSession retrieves an existing session or initializes a new one.
func (d *Daemon) GetOrCreateSession(name string, cols, rows int) (*Session, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if s, exists := d.Sessions[name]; exists {
		return s, nil
	}

	// Try restoring from persisted session state
	if st, err := state.LoadStateFromFile(""); err == nil && st != nil {
		if ss, ok := st.Sessions[name]; ok {
			s, err := RestoreSessionFromSerializedWithConfig(ss, cols, rows, d.Config, func(state protocol.LayoutStatePayload) {
				d.broadcastLayoutState(name, state)
			})
			if err == nil {
				d.Sessions[name] = s
				return s, nil
			}
		}
	}

	s, err := NewSessionWithConfig(name, cols, rows, d.Config, func(state protocol.LayoutStatePayload) {
		d.broadcastLayoutState(name, state)
	})
	if err != nil {
		return nil, err
	}

	d.Sessions[name] = s
	return s, nil
}

func (d *Daemon) handleConnection(conn net.Conn) {
	defer conn.Close()

	var attachedSession *Session
	reader := protocol.NewEnvelopeReader(conn)
	writer := protocol.NewEnvelopeWriter(conn)

	for {
		env, err := reader.Read()
		if err != nil {
			break
		}

		switch env.Type {
		case protocol.MsgAttach:
			var p protocol.AttachPayload
			if err := json.Unmarshal(env.Payload, &p); err == nil {
				sessName := p.SessionName
				if sessName == "" {
					sessName = d.DefaultSession
				}
				session, err := d.GetOrCreateSession(sessName, p.Cols, p.Rows)
				if err == nil {
					attachedSession = session
					d.mu.Lock()
					d.clients[conn] = &clientEntry{
						session: session,
						writer:  writer,
						cols:    p.Cols,
						rows:    p.Rows,
					}
					d.mu.Unlock()

					// Clamp session dimensions to minimum across all attached clients
					d.updateSessionDimensions(session)

					// Send current layout state to newly attached client
					state := session.GetLayoutState()
					data, _ := json.Marshal(state)
					_ = writer.Write(protocol.Envelope{
						Type:    protocol.MsgLayoutState,
						Payload: data,
					})

					// Forward PTY output from active pane to this client
					go d.streamPaneOutput(writer, session)
				}
			}

		case protocol.MsgInput:
			var p protocol.InputPayload
			if err := json.Unmarshal(env.Payload, &p); err == nil {
				if env.PaneID != "" {
					d.mu.RLock()
					var targetPane *Pane
					for _, s := range d.Sessions {
						if p := s.GetPane(env.PaneID); p != nil {
							targetPane = p
							break
						}
					}
					d.mu.RUnlock()
					if targetPane != nil {
						_, _ = targetPane.Write(p.Data)
					}
				} else if attachedSession != nil {
					if active := attachedSession.ActivePane(); active != nil {
						_, _ = active.Write(p.Data)
					}
				}
			}

		case protocol.MsgResize:
			if attachedSession != nil {
				var p protocol.ResizePayload
				if err := json.Unmarshal(env.Payload, &p); err == nil {
					d.mu.Lock()
					if entry, ok := d.clients[conn]; ok {
						entry.cols = p.Cols
						entry.rows = p.Rows
					}
					d.mu.Unlock()
					d.updateSessionDimensions(attachedSession)
				}
			}

		case protocol.MsgAction:
			if attachedSession != nil {
				var p protocol.ActionPayload
				if err := json.Unmarshal(env.Payload, &p); err == nil {
					d.handleAction(attachedSession, p)
				}
			}

		case protocol.MsgAgentStatus:
			sess := attachedSession
			if sess == nil {
				sess = d.getSessionForRequest(env.SessionID)
			}
			if sess != nil {
				var p protocol.AgentStatusPayload
				if err := json.Unmarshal(env.Payload, &p); err == nil {
					sess.Broker.UpdateStatus(env.PaneID, p)
				}
			}

		case protocol.MsgAgentSend:
			sess := attachedSession
			if sess == nil {
				sess = d.getSessionForRequest(env.SessionID)
			}
			if sess != nil {
				var p protocol.AgentMessagePayload
				if err := json.Unmarshal(env.Payload, &p); err == nil {
					msg := sess.Broker.Send(p.FromPaneID, p.ToTarget, p.Content)
					data, _ := json.Marshal(msg)
					_ = writer.Write(protocol.Envelope{
						Type:    protocol.MsgAgentSend,
						Payload: data,
					})
				}
			}

		case protocol.MsgPaneCapture:
			sess := attachedSession
			if sess == nil {
				sess = d.getSessionForRequest(env.SessionID)
			}
			if sess != nil {
				var p protocol.PaneCaptureRequest
				if err := json.Unmarshal(env.Payload, &p); err == nil {
					sess.mu.RLock()
					targetPane := sess.Panes[p.TargetPaneID]
					sess.mu.RUnlock()

					var lines []string
					if targetPane != nil {
						lines = targetPane.CaptureLastLines(p.Lines)
					}
					res := protocol.PaneCaptureResponse{
						TargetPaneID: p.TargetPaneID,
						Lines:        lines,
					}
					data, _ := json.Marshal(res)
					_ = writer.Write(protocol.Envelope{
						Type:    protocol.MsgPaneCaptureRes,
						Payload: data,
					})
				}
			}

		case protocol.MsgListSessions:
			d.mu.RLock()
			var summaries []protocol.SessionSummary
			for name, sess := range d.Sessions {
				sess.mu.RLock()
				wsCount := len(sess.Workspaces)
				panesCount := len(sess.Panes)
				activeWS := sess.ActiveWS
				sess.mu.RUnlock()

				clientCount := 0
				for _, entry := range d.clients {
					if entry.session != nil && entry.session.Name == name {
						clientCount++
					}
				}

				summaries = append(summaries, protocol.SessionSummary{
					Name:            name,
					WorkspacesCount: wsCount,
					PanesCount:      panesCount,
					ActiveWorkspace: activeWS,
					AttachedClients: clientCount,
				})
			}
			d.mu.RUnlock()

			resData, _ := json.Marshal(protocol.ListSessionsResponse{Sessions: summaries})
			_ = writer.Write(protocol.Envelope{
				Type:    protocol.MsgListSessionsRes,
				Payload: resData,
			})

		case protocol.MsgListPanes:
			var req protocol.ListPanesRequest
			_ = json.Unmarshal(env.Payload, &req)

			d.mu.RLock()
			var panes []protocol.DetailedPaneInfo
			for name, sess := range d.Sessions {
				if req.SessionName != "" && req.SessionName != name {
					continue
				}
				sess.mu.RLock()
				for wsID, ws := range sess.Workspaces {
					ws.mu.RLock()
					activePaneID := ws.Tree.ActivePaneID
					for _, pid := range ws.Tree.ListLeafPanes() {
						node := ws.Tree.FindNode(pid)
						pane := sess.Panes[pid]
						if node != nil && pane != nil {
							pane.mu.RLock()
							panes = append(panes, protocol.DetailedPaneInfo{
								SessionName: name,
								WorkspaceID: wsID,
								PaneID:      pid,
								Title:       pane.Title,
								Cwd:         pane.Cwd,
								IsActive:    pid == activePaneID && sess.ActiveWS == wsID,
								AgentState:  pane.AgentState,
								AgentBadge:  pane.AgentBadge,
								Width:       node.Rect.W,
								Height:      node.Rect.H,
							})
							pane.mu.RUnlock()
						}
					}
					ws.mu.RUnlock()
				}
				sess.mu.RUnlock()
			}
			d.mu.RUnlock()

			resData, _ := json.Marshal(protocol.ListPanesResponse{Panes: panes})
			_ = writer.Write(protocol.Envelope{
				Type:    protocol.MsgListPanesRes,
				Payload: resData,
			})

		case protocol.MsgSpawnPane:
			var req protocol.SpawnPaneRequest
			_ = json.Unmarshal(env.Payload, &req)

			sessionName := req.SessionName
			if sessionName == "" {
				sessionName = "main"
			}

			d.mu.RLock()
			sess := d.Sessions[sessionName]
			d.mu.RUnlock()

			if sess == nil {
				resData, _ := json.Marshal(protocol.SpawnPaneResponse{
					Success: false,
					Error:   fmt.Sprintf("session %q not found", sessionName),
				})
				_ = writer.Write(protocol.Envelope{
					Type:    protocol.MsgSpawnPaneRes,
					Payload: resData,
				})
				continue
			}

			dir := bsp.SplitVertical
			if req.Direction == "horizontal" {
				dir = bsp.SplitHorizontal
			}

			newPane, err := sess.SpawnPane(dir, req.Cwd, req.Command)
			if err != nil {
				resData, _ := json.Marshal(protocol.SpawnPaneResponse{
					Success: false,
					Error:   err.Error(),
				})
				_ = writer.Write(protocol.Envelope{
					Type:    protocol.MsgSpawnPaneRes,
					Payload: resData,
				})
			} else {
				resData, _ := json.Marshal(protocol.SpawnPaneResponse{
					Success: true,
					PaneID:  newPane.ID,
				})
				_ = writer.Write(protocol.Envelope{
					Type:    protocol.MsgSpawnPaneRes,
					Payload: resData,
				})
			}

		case protocol.MsgClosePane:
			var req protocol.ClosePaneRequest
			_ = json.Unmarshal(env.Payload, &req)

			if req.PaneID == "" {
				resData, _ := json.Marshal(protocol.ClosePaneResponse{
					Success: false,
					Error:   "pane_id is required",
				})
				_ = writer.Write(protocol.Envelope{
					Type:    protocol.MsgClosePaneRes,
					Payload: resData,
				})
				continue
			}

			err := d.ClosePaneByID(req.PaneID)
			errStr := ""
			if err != nil {
				errStr = err.Error()
			}
			resData, _ := json.Marshal(protocol.ClosePaneResponse{
				Success: err == nil,
				Error:   errStr,
			})
			_ = writer.Write(protocol.Envelope{
				Type:    protocol.MsgClosePaneRes,
				Payload: resData,
			})

		case protocol.MsgKillSession:
			var req protocol.KillSessionRequest
			_ = json.Unmarshal(env.Payload, &req)

			if req.SessionName == "" {
				resData, _ := json.Marshal(protocol.KillSessionResponse{
					Success: false,
					Error:   "session_name is required",
				})
				_ = writer.Write(protocol.Envelope{
					Type:    protocol.MsgKillSessionRes,
					Payload: resData,
				})
				continue
			}

			err := d.KillSession(req.SessionName)
			errStr := ""
			if err != nil {
				errStr = err.Error()
			}
			resData, _ := json.Marshal(protocol.KillSessionResponse{
				Success: err == nil,
				Error:   errStr,
			})
			_ = writer.Write(protocol.Envelope{
				Type:    protocol.MsgKillSessionRes,
				Payload: resData,
			})

		case protocol.MsgRenameSession:
			var req protocol.RenameSessionRequest
			_ = json.Unmarshal(env.Payload, &req)

			if req.OldName == "" || req.NewName == "" {
				resData, _ := json.Marshal(protocol.RenameSessionResponse{
					Success: false,
					Error:   "old_name and new_name are required",
				})
				_ = writer.Write(protocol.Envelope{
					Type:    protocol.MsgRenameSessionRes,
					Payload: resData,
				})
				continue
			}

			err := d.RenameSession(req.OldName, req.NewName)
			errStr := ""
			if err != nil {
				errStr = err.Error()
			}
			resData, _ := json.Marshal(protocol.RenameSessionResponse{
				Success: err == nil,
				Error:   errStr,
			})
			_ = writer.Write(protocol.Envelope{
				Type:    protocol.MsgRenameSessionRes,
				Payload: resData,
			})

		case protocol.MsgRenameWorkspace:
			var req protocol.RenameWorkspaceRequest
			_ = json.Unmarshal(env.Payload, &req)

			if req.NewName == "" {
				resData, _ := json.Marshal(protocol.RenameWorkspaceResponse{
					Success: false,
					Error:   "new_name is required",
				})
				_ = writer.Write(protocol.Envelope{
					Type:    protocol.MsgRenameWorkspaceRes,
					Payload: resData,
				})
				continue
			}

			err := d.RenameWorkspace(req.SessionName, req.WorkspaceID, req.NewName)
			errStr := ""
			if err != nil {
				errStr = err.Error()
			}
			resData, _ := json.Marshal(protocol.RenameWorkspaceResponse{
				Success: err == nil,
				Error:   errStr,
			})
			_ = writer.Write(protocol.Envelope{
				Type:    protocol.MsgRenameWorkspaceRes,
				Payload: resData,
			})

		case protocol.MsgStopServer:
			_ = writer.Write(protocol.Envelope{
				Type:    protocol.MsgStopServerRes,
				Payload: []byte(`"ok"`),
			})
			go func() {
				time.Sleep(50 * time.Millisecond)
				d.Stop()
				os.Exit(0)
			}()
			return

		case protocol.MsgDetach:
			return
		}
	}

	d.mu.Lock()
	delete(d.clients, conn)
	d.mu.Unlock()

	if attachedSession != nil {
		d.updateSessionDimensions(attachedSession)
	}
}

func (d *Daemon) getSessionForRequest(sessionID string) *Session {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if sessionID != "" {
		if s, ok := d.Sessions[sessionID]; ok {
			return s
		}
	}
	return d.Sessions[d.DefaultSession]
}

func (d *Daemon) streamPaneOutput(writer *protocol.EnvelopeWriter, s *Session) {
	type paneUpdate struct {
		paneID string
		data   []byte
	}

	type paneSub struct {
		ch   chan []byte
		stop chan struct{}
	}

	updateCh := make(chan paneUpdate, 128)
	subscribed := make(map[string]*paneSub)

	subscribe := func(p *Pane) {
		if p == nil {
			return
		}
		if _, exists := subscribed[p.ID]; exists {
			return
		}
		subCh := make(chan []byte, 64)
		stopCh := make(chan struct{})
		subscribed[p.ID] = &paneSub{ch: subCh, stop: stopCh}
		p.AddListener(subCh)

		// Start background pump from pane listener into updateCh
		go func(paneID string, ch <-chan []byte, stop <-chan struct{}) {
			for {
				select {
				case <-stop:
					return
				case chunk, ok := <-ch:
					if !ok {
						return
					}
					select {
					case updateCh <- paneUpdate{paneID: paneID, data: chunk}:
					case <-stop:
						return
					}
				}
			}
		}(p.ID, subCh, stopCh)

		// Immediately flush recent ANSI buffer so client sees shell prompt/content
		history := p.GetRawBuffer()
		if len(history) > 0 {
			f := protocol.FrameUpdatePayload{
				PaneID: p.ID,
				ANSI:   history,
			}
			payload, _ := json.Marshal(f)
			_ = writer.Write(protocol.Envelope{
				Type:    protocol.MsgFrameUpdate,
				PaneID:  p.ID,
				Payload: payload,
			})
		}
	}

	unsubscribe := func(paneID string) {
		sub, ok := subscribed[paneID]
		if !ok {
			return
		}
		delete(subscribed, paneID)
		if p := s.GetPane(paneID); p != nil {
			p.RemoveListener(sub.ch)
		}
		close(sub.stop)
	}

	// Initial subscription to all visible panes in active workspace
	for _, p := range s.VisiblePanes() {
		subscribe(p)
	}

	defer func() {
		for pid, sub := range subscribed {
			if p := s.GetPane(pid); p != nil {
				p.RemoveListener(sub.ch)
			}
			close(sub.stop)
		}
	}()

	ticker := time.NewTicker(60 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case update, ok := <-updateCh:
			if !ok {
				return
			}
			f := protocol.FrameUpdatePayload{
				PaneID: update.paneID,
				ANSI:   update.data,
			}
			payload, _ := json.Marshal(f)
			if err := writer.Write(protocol.Envelope{
				Type:    protocol.MsgFrameUpdate,
				PaneID:  update.paneID,
				Payload: payload,
			}); err != nil {
				return
			}

		case <-ticker.C:
			// Automatically track visible pane changes (navigation, splits, closes, monocle, workspaces)
			currentVisible := s.VisiblePanes()
			visibleMap := make(map[string]*Pane, len(currentVisible))
			for _, p := range currentVisible {
				visibleMap[p.ID] = p
				if _, ok := subscribed[p.ID]; !ok {
					subscribe(p)
				}
			}
			for pid := range subscribed {
				if _, ok := visibleMap[pid]; !ok {
					unsubscribe(pid)
				}
			}
		}
	}
}

func (d *Daemon) handleAction(s *Session, action protocol.ActionPayload) {
	switch action.Action {
	case "split_h":
		_, _ = s.SplitActive(bsp.SplitHorizontal)
	case "split_v":
		_, _ = s.SplitActive(bsp.SplitVertical)
	case "split_auto":
		_, _ = s.SplitActiveAuto()
	case "close_pane":
		if paneID := action.Params["pane_id"]; paneID != "" {
			_ = s.ClosePaneByID(paneID)
		} else {
			_ = s.CloseActive()
		}
	case "nav_left":
		s.Navigate(bsp.NavLeft)
	case "nav_down":
		s.Navigate(bsp.NavDown)
	case "nav_up":
		s.Navigate(bsp.NavUp)
	case "nav_right":
		s.Navigate(bsp.NavRight)
	case "focus_pane":
		paneID := action.Params["pane_id"]
		if paneID != "" {
			s.FocusPane(paneID)
		}
	case "swap_next":
		_ = s.SwapActiveWithNext()
	case "swap_left":
		_ = s.SwapActive(bsp.NavLeft)
	case "swap_down":
		_ = s.SwapActive(bsp.NavDown)
	case "swap_up":
		_ = s.SwapActive(bsp.NavUp)
	case "swap_right":
		_ = s.SwapActive(bsp.NavRight)
	case "move_pane_ws":
		var wsNum int
		if fmt.Sscanf(action.Params["workspace"], "%d", &wsNum); wsNum > 0 {
			_ = s.MoveActiveToWorkspace(wsNum)
		}
	case "toggle_monocle":
		s.ToggleMonocle()
	case "switch_workspace":
		var wsNum int
		if fmt.Sscanf(action.Params["workspace"], "%d", &wsNum); wsNum > 0 {
			s.SwitchWorkspace(wsNum)
		}
	case "resize_delta":
		var delta float64
		if fmt.Sscanf(action.Params["delta"], "%f", &delta); delta != 0 {
			s.AdjustSplit(delta)
		}
	case "mode_change":
		target := protocol.Mode(action.Params["target"])
		if target != "" {
			s.SetMode(target)
		}
	case "cycle_layout":
		s.CycleLayout()
	case "set_layout":
		lt := bsp.LayoutType(action.Params["type"])
		if lt != "" {
			s.SetLayout(lt)
		}
	case "rename_workspace":
		name := action.Params["name"]
		if name != "" {
			wsNum := s.ActiveWS
			if action.Params["workspace"] != "" {
				_, _ = fmt.Sscanf(action.Params["workspace"], "%d", &wsNum)
			}
			s.RenameWorkspace(wsNum, name)
		}
	case "redraw":
		s.NotifyState()
	}
}

func (d *Daemon) updateSessionDimensions(s *Session) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	minCols := 0
	minRows := 0
	count := 0

	for _, entry := range d.clients {
		if entry.session == s && entry.cols > 0 && entry.rows > 0 {
			if count == 0 || entry.cols < minCols {
				minCols = entry.cols
			}
			if count == 0 || entry.rows < minRows {
				minRows = entry.rows
			}
			count++
		}
	}

	if count > 0 && minCols > 0 && minRows > 0 {
		s.ResizeTerminal(minCols, minRows)
	}
}

func (d *Daemon) broadcastLayoutState(sessionName string, state protocol.LayoutStatePayload) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	data, err := json.Marshal(state)
	if err != nil {
		return
	}

	env := protocol.Envelope{
		Type:    protocol.MsgLayoutState,
		Payload: data,
	}

	for _, entry := range d.clients {
		if entry.session.Name == sessionName {
			_ = entry.writer.Write(env)
		}
	}
}

// SaveState serializes all active sessions to disk.
func (d *Daemon) SaveState(path string) error {
	d.mu.RLock()
	defer d.mu.RUnlock()

	st := &state.SerializedState{
		Version:  1,
		SavedAt:  time.Now(),
		Sessions: make(map[string]*state.SerializedSession),
	}

	for name, sess := range d.Sessions {
		st.Sessions[name] = sess.ToSerialized()
	}

	return state.SaveStateToFile(path, st)
}

// ClosePaneByID finds the session containing paneID and terminates the pane.
func (d *Daemon) ClosePaneByID(paneID string) error {
	d.mu.RLock()
	var targetSession *Session
	for _, sess := range d.Sessions {
		sess.mu.RLock()
		_, exists := sess.Panes[paneID]
		sess.mu.RUnlock()
		if exists {
			targetSession = sess
			break
		}
	}
	d.mu.RUnlock()

	if targetSession == nil {
		return fmt.Errorf("pane %q not found", paneID)
	}

	return targetSession.ClosePaneByID(paneID)
}

// KillSession terminates all panes in a session, disconnects attached clients, and removes it.
func (d *Daemon) KillSession(name string) error {
	d.mu.Lock()
	sess, ok := d.Sessions[name]
	if !ok {
		d.mu.Unlock()
		return fmt.Errorf("session %q not found", name)
	}

	// Terminate all panes in the session
	sess.Close()

	// Detach connected clients
	for conn, entry := range d.clients {
		if entry.session != nil && entry.session.Name == name {
			_ = entry.writer.Write(protocol.Envelope{
				Type: protocol.MsgBell,
			})
			_ = entry.writer.Write(protocol.Envelope{
				Type:    protocol.MsgError,
				Payload: []byte(fmt.Sprintf("%q", fmt.Sprintf("session %s was terminated", name))),
			})
			delete(d.clients, conn)
			_ = conn.Close()
		}
	}

	delete(d.Sessions, name)
	d.mu.Unlock()

	_ = d.SaveState("")
	return nil
}

// RenameSession updates the identifier of an active session and preserves connected clients.
func (d *Daemon) RenameSession(oldName, newName string) error {
	d.mu.Lock()

	newName = strings.TrimSpace(newName)
	if newName == "" {
		d.mu.Unlock()
		return fmt.Errorf("new session name cannot be empty")
	}
	if oldName == newName {
		d.mu.Unlock()
		return nil
	}

	sess, ok := d.Sessions[oldName]
	if !ok {
		d.mu.Unlock()
		return fmt.Errorf("session %q not found", oldName)
	}

	if _, exists := d.Sessions[newName]; exists {
		d.mu.Unlock()
		return fmt.Errorf("session %q already exists", newName)
	}

	sess.mu.Lock()
	sess.Name = newName
	sess.mu.Unlock()

	delete(d.Sessions, oldName)
	d.Sessions[newName] = sess

	if d.DefaultSession == oldName {
		d.DefaultSession = newName
	}

	d.mu.Unlock()

	// Notify attached clients of updated session name
	go sess.NotifyState()

	_ = d.SaveState("")
	return nil
}

// RenameWorkspace assigns a new display name to a workspace within a session.
func (d *Daemon) RenameWorkspace(sessionName string, wsID int, newName string) error {
	d.mu.RLock()
	if sessionName == "" {
		sessionName = d.DefaultSession
	}
	sess, ok := d.Sessions[sessionName]
	d.mu.RUnlock()

	if !ok {
		return fmt.Errorf("session %q not found", sessionName)
	}

	if wsID <= 0 {
		wsID = sess.ActiveWS
	}

	sess.RenameWorkspace(wsID, newName)
	return nil
}

// RestoreSessions reads the persisted session snapshot and restores sessions into memory.
func (d *Daemon) RestoreSessions(path string) error {
	st, err := state.LoadStateFromFile(path)
	if err != nil {
		return err
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	for name, ss := range st.Sessions {
		if _, exists := d.Sessions[name]; !exists {
			sess, err := RestoreSessionFromSerializedWithConfig(ss, 80, 24, d.Config, func(lsp protocol.LayoutStatePayload) {
				d.broadcastLayoutState(name, lsp)
			})
			if err == nil {
				d.Sessions[name] = sess
			}
		}
	}

	return nil
}

// Stop cleanly closes sockets and terminates child processes, persisting state first.
func (d *Daemon) Stop() {
	d.mu.Lock()
	d.isClosing = true
	d.mu.Unlock()

	// Automatically persist state on stop
	_ = d.SaveState("")

	if d.listener != nil {
		_ = d.listener.Close()
	}
	_ = os.Remove(d.SocketPath)

	d.mu.Lock()
	sessions := make([]*Session, 0, len(d.Sessions))
	for _, sess := range d.Sessions {
		sessions = append(sessions, sess)
	}
	d.Sessions = make(map[string]*Session)
	d.mu.Unlock()

	for _, sess := range sessions {
		sess.Close()
	}
}
