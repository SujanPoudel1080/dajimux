package daemon

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/SujanPoudel1080/dajimux/internal/bsp"
	"github.com/SujanPoudel1080/dajimux/internal/protocol"
)

// Daemon coordinates sessions, clients, and IPC sockets.
type Daemon struct {
	mu            sync.RWMutex
	SocketPath    string
	listener      net.Listener
	Sessions      map[string]*Session
	DefaultSession string
	clients       map[net.Conn]*Session
	isClosing     bool
}

// DefaultSocketPath returns the standard socket location for the current user.
func DefaultSocketPath() string {
	uid := os.Getuid()
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("dajimux-%d", uid))
	_ = os.MkdirAll(dir, 0700)
	return filepath.Join(dir, "daemon.sock")
}

// NewDaemon initializes a daemon server.
func NewDaemon(socketPath string) *Daemon {
	if socketPath == "" {
		socketPath = DefaultSocketPath()
	}
	return &Daemon{
		SocketPath:     socketPath,
		Sessions:       make(map[string]*Session),
		DefaultSession: "main",
		clients:        make(map[net.Conn]*Session),
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

	s, err := NewSession(name, cols, rows, func(state protocol.LayoutStatePayload) {
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

	for {
		env, err := protocol.ReadEnvelope(conn)
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
					d.clients[conn] = session
					d.mu.Unlock()

					// Send current layout state to newly attached client
					state := session.GetLayoutState()
					data, _ := json.Marshal(state)
					_ = protocol.WriteEnvelope(conn, protocol.Envelope{
						Type:    protocol.MsgLayoutState,
						Payload: data,
					})

					// Forward PTY output from active pane to this client
					go d.streamPaneOutput(conn, session)
				}
			}

		case protocol.MsgInput:
			if attachedSession != nil {
				var p protocol.InputPayload
				if err := json.Unmarshal(env.Payload, &p); err == nil {
					if active := attachedSession.ActivePane(); active != nil {
						_, _ = active.Write(p.Data)
					}
				}
			}

		case protocol.MsgResize:
			if attachedSession != nil {
				var p protocol.ResizePayload
				if err := json.Unmarshal(env.Payload, &p); err == nil {
					attachedSession.ResizeTerminal(p.Cols, p.Rows)
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
			if attachedSession != nil {
				var p protocol.AgentStatusPayload
				if err := json.Unmarshal(env.Payload, &p); err == nil {
					attachedSession.Broker.UpdateStatus(env.PaneID, p)
				}
			}

		case protocol.MsgAgentSend:
			if attachedSession != nil {
				var p protocol.AgentMessagePayload
				if err := json.Unmarshal(env.Payload, &p); err == nil {
					msg := attachedSession.Broker.Send(p.FromPaneID, p.ToTarget, p.Content)
					data, _ := json.Marshal(msg)
					_ = protocol.WriteEnvelope(conn, protocol.Envelope{
						Type:    protocol.MsgAgentSend,
						Payload: data,
					})
				}
			}

		case protocol.MsgPaneCapture:
			if attachedSession != nil {
				var p protocol.PaneCaptureRequest
				if err := json.Unmarshal(env.Payload, &p); err == nil {
					attachedSession.mu.RLock()
					targetPane := attachedSession.Panes[p.TargetPaneID]
					attachedSession.mu.RUnlock()

					var lines []string
					if targetPane != nil {
						lines = targetPane.CaptureLastLines(p.Lines)
					}
					res := protocol.PaneCaptureResponse{
						TargetPaneID: p.TargetPaneID,
						Lines:        lines,
					}
					data, _ := json.Marshal(res)
					_ = protocol.WriteEnvelope(conn, protocol.Envelope{
						Type:    protocol.MsgPaneCaptureRes,
						Payload: data,
					})
				}
			}

		case protocol.MsgDetach:
			return
		}
	}

	d.mu.Lock()
	delete(d.clients, conn)
	d.mu.Unlock()
}

func (d *Daemon) streamPaneOutput(conn net.Conn, s *Session) {
	for {
		active := s.ActivePane()
		if active == nil {
			break
		}

		select {
		case data, ok := <-active.OutputCh:
			if !ok {
				return
			}
			f := protocol.FrameUpdatePayload{ANSI: data}
			payload, _ := json.Marshal(f)
			err := protocol.WriteEnvelope(conn, protocol.Envelope{
				Type:    protocol.MsgFrameUpdate,
				Payload: payload,
			})
			if err != nil {
				return
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
	case "close_pane":
		_ = s.CloseActive()
	case "nav_left":
		s.Navigate(bsp.NavLeft)
	case "nav_down":
		s.Navigate(bsp.NavDown)
	case "nav_up":
		s.Navigate(bsp.NavUp)
	case "nav_right":
		s.Navigate(bsp.NavRight)
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

	for conn, sess := range d.clients {
		if sess.Name == sessionName {
			_ = protocol.WriteEnvelope(conn, env)
		}
	}
}

// Stop cleanly closes sockets and terminates child processes.
func (d *Daemon) Stop() {
	d.mu.Lock()
	d.isClosing = true
	d.mu.Unlock()

	if d.listener != nil {
		_ = d.listener.Close()
	}
	_ = os.Remove(d.SocketPath)

	d.mu.RLock()
	defer d.mu.RUnlock()
	for _, sess := range d.Sessions {
		for _, pane := range sess.Panes {
			_ = pane.Close()
		}
	}
}
