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

	"github.com/SujanPoudel1080/dajimux/internal/config"
	"github.com/SujanPoudel1080/dajimux/internal/protocol"
	"github.com/SujanPoudel1080/dajimux/internal/state"
)

// Daemon coordinates sessions, clients, and IPC sockets.
type Daemon struct {
	mu               sync.RWMutex
	SocketPath       string
	listener         net.Listener
	Sessions         map[string]*Session
	DefaultSession   string
	Config           *config.Config
	clients          map[net.Conn]*clientEntry
	isClosing        bool
	actionDispatcher *ActionDispatcher
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
		SocketPath:       socketPath,
		Sessions:         make(map[string]*Session),
		DefaultSession:   "main",
		Config:           cfg,
		clients:          make(map[net.Conn]*clientEntry),
		actionDispatcher: NewActionDispatcher(),
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
				s.OnTerminate = func(sessionName string) {
					go func() {
						_ = d.KillSession(sessionName)
					}()
				}
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

	s.OnTerminate = func(sessionName string) {
		go func() {
			_ = d.KillSession(sessionName)
		}()
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
			session, err := d.handleAttachMsg(conn, writer, env)
			if err == nil {
				attachedSession = session
			}

		case protocol.MsgInput:
			d.handleInputMsg(attachedSession, env)

		case protocol.MsgResize:
			d.handleResizeMsg(conn, attachedSession, env)

		case protocol.MsgAction:
			if attachedSession != nil {
				var p protocol.ActionPayload
				if err := json.Unmarshal(env.Payload, &p); err == nil {
					_ = d.actionDispatcher.Dispatch(attachedSession, p)
				}
			}

		case protocol.MsgAgentStatus:
			d.handleAgentStatusMsg(attachedSession, env)

		case protocol.MsgAgentSend:
			d.handleAgentSendMsg(attachedSession, writer, env)

		case protocol.MsgPaneCapture:
			d.handlePaneCaptureMsg(attachedSession, writer, env)

		case protocol.MsgListSessions:
			d.handleListSessionsMsg(writer)

		case protocol.MsgListPanes:
			d.handleListPanesMsg(writer, env)

		case protocol.MsgSpawnPane:
			d.handleSpawnPaneMsg(writer, env)

		case protocol.MsgClosePane:
			d.handleClosePaneMsg(writer, env)

		case protocol.MsgKillSession:
			d.handleKillSessionMsg(writer, env)

		case protocol.MsgRenameSession:
			d.handleRenameSessionMsg(writer, env)

		case protocol.MsgRenameWorkspace:
			d.handleRenameWorkspaceMsg(writer, env)

		case protocol.MsgStopServer:
			d.handleStopServerMsg(writer)
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
	_ = d.actionDispatcher.Dispatch(s, action)
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
	data, err := json.Marshal(state)
	if err != nil {
		return
	}

	env := protocol.Envelope{
		Type:    protocol.MsgLayoutState,
		Payload: data,
	}

	d.mu.RLock()
	var targets []*protocol.EnvelopeWriter
	for _, entry := range d.clients {
		if entry.session != nil && entry.session.Name == sessionName {
			targets = append(targets, entry.writer)
		}
	}
	d.mu.RUnlock()

	for _, w := range targets {
		_ = w.Write(env)
	}
}

// SaveState serializes all active sessions to disk.
func (d *Daemon) SaveState(path string) error {
	d.mu.RLock()
	st := &state.SerializedState{
		Version:  1,
		SavedAt:  time.Now(),
		Sessions: make(map[string]*state.SerializedSession, len(d.Sessions)),
	}

	for name, sess := range d.Sessions {
		st.Sessions[name] = sess.ToSerialized()
	}
	d.mu.RUnlock()

	return state.SaveStateToFile(path, st)
}

// ClosePaneByID finds the session containing paneID and terminates the pane.
func (d *Daemon) ClosePaneByID(paneID string) error {
	d.mu.RLock()
	var targetSession *Session
	for _, sess := range d.Sessions {
		if sess.GetPane(paneID) != nil {
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

	// Terminate all panes in the session
	sess.Close()

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

	delete(d.Sessions, oldName)
	d.Sessions[newName] = sess

	if d.DefaultSession == oldName {
		d.DefaultSession = newName
	}

	d.mu.Unlock()

	sess.Rename(newName)

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
				sess.OnTerminate = func(sessionName string) {
					go func() {
						_ = d.KillSession(sessionName)
					}()
				}
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

	// Detach all connected clients before closing listener
	for conn, entry := range d.clients {
		_ = entry.writer.Write(protocol.Envelope{
			Type:    protocol.MsgError,
			Payload: []byte(`"dajimux daemon stopped"`),
		})
		_ = conn.Close()
	}
	d.clients = make(map[net.Conn]*clientEntry)
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
