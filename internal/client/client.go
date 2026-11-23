package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/SujanPoudel1080/dajimux/internal/config"
	"github.com/SujanPoudel1080/dajimux/internal/protocol"
	"github.com/SujanPoudel1080/dajimux/internal/sysstats"
)

// DragPoint tracks coordinate origin during mouse split dragging.
type DragPoint struct {
	X int
	Y int
}

// Client manages the terminal UI, raw input mode, and IPC connection to the daemon.
type Client struct {
	mu                sync.Mutex
	SocketPath        string
	SessionName       string
	Config            *config.Config
	conn              net.Conn
	inputHandler      *InputHandler
	palette           *Palette
	paletteView       *PaletteOverlay
	helpModal         *HelpModal
	scrollback        *ScrollbackViewer
	chrome            *ChromeRenderer
	lastState         protocol.LayoutStatePayload
	cols              int
	rows              int
	origTerm          *syscall.Termios
	isRunning         bool
	dragStart         *DragPoint
	compositors       map[string]*ANSICompositor
	sysStatsCollector *sysstats.Collector
	disconnectMsg     string
}

// NewClient initializes a client for the target session.
func NewClient(socketPath, sessionName string) *Client {
	return NewClientWithConfig(socketPath, sessionName, config.DefaultConfig())
}

// NewClientWithConfig initializes a client with the provided user configuration.
func NewClientWithConfig(socketPath, sessionName string, cfg *config.Config) *Client {
	if sessionName == "" {
		sessionName = "main"
	}
	if cfg == nil {
		cfg = config.DefaultConfig()
	}

	handler := NewInputHandler()
	if cfg.Input.ModalDoubleTapMs > 0 {
		handler.SetDoubleTapThreshold(time.Duration(cfg.Input.ModalDoubleTapMs) * time.Millisecond)
	}
	handler.SetPrefixMode(cfg.Input.PrefixMode, cfg.Input.CustomLeader)

	var statsCol *sysstats.Collector
	if cfg.General.ShowSystemStats {
		statsCol = sysstats.NewCollector(2 * time.Second)
	}

	return &Client{
		SocketPath:        socketPath,
		SessionName:       sessionName,
		Config:            cfg,
		inputHandler:      handler,
		palette:           NewPalette(),
		paletteView:       NewPaletteOverlay(),
		helpModal:         NewHelpModal(),
		scrollback:        NewScrollbackViewer(),
		chrome:            NewChromeRenderer(cfg, statsCol),
		compositors:       make(map[string]*ANSICompositor),
		sysStatsCollector: statsCol,
	}
}

// Run attaches to the daemon and enters the interactive raw terminal loop.
func (c *Client) Run() error {
	conn, err := net.Dial("unix", c.SocketPath)
	if err != nil {
		return fmt.Errorf("connect to daemon at %s: %w", c.SocketPath, err)
	}
	return c.RunWithConn(conn)
}

// RunWithConn attaches to the daemon using an established network connection.
func (c *Client) RunWithConn(conn net.Conn) error {
	c.conn = conn
	defer conn.Close()

	// Query terminal size
	cols, rows, err := getTermSize()
	if err != nil {
		cols, rows = 80, 24
	}
	c.cols = cols
	c.rows = rows

	// Put terminal into raw mode
	oldState, err := enableRawMode(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("enable raw mode: %w", err)
	}
	c.origTerm = oldState
	defer c.restoreTerminal()

	c.isRunning = true

	// Clear screen and enter alternate screen buffer with full scroll margins
	fmt.Print("\033[?1049h\033[H\033[2J\033[r")
	if c.Config == nil || c.Config.General.MouseSupport {
		fmt.Print("\033[?1000h\033[?1002h\033[?1006h")
	}

	// Send Attach Envelope
	attachData, _ := json.Marshal(protocol.AttachPayload{
		SessionName: c.SessionName,
		Cols:        c.cols,
		Rows:        c.rows,
		Term:        os.Getenv("TERM"),
	})
	if err := protocol.WriteEnvelope(conn, protocol.Envelope{
		Type:    protocol.MsgAttach,
		Payload: attachData,
	}); err != nil {
		return fmt.Errorf("attach: %w", err)
	}

	// Handle termination signals to cleanly restore terminal raw mode on Linux/macOS
	termSigCh := make(chan os.Signal, 1)
	signal.Notify(termSigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		<-termSigCh
		c.restoreTerminal()
		os.Exit(0)
	}()

	// Handle SIGWINCH (terminal resize)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGWINCH)
	go func() {
		for range sigCh {
			nc, nr, err := getTermSize()
			if err == nil {
				c.mu.Lock()
				c.cols = nc
				c.rows = nr
				c.mu.Unlock()

				// Clear terminal screen and ensure full scroll margins on resize to eliminate stale borders
				os.Stdout.WriteString("\033[H\033[2J\033[r")

				rp, _ := json.Marshal(protocol.ResizePayload{Cols: nc, Rows: nr})
				_ = protocol.WriteEnvelope(conn, protocol.Envelope{
					Type:    protocol.MsgResize,
					Payload: rp,
				})
			}
		}
	}()

	// Daemon -> Client read loop
	go c.readDaemonLoop()

	// Periodic system telemetry ticker (CPU/RAM)
	if c.sysStatsCollector != nil {
		c.sysStatsCollector.Start(func(st sysstats.Stats) {
			c.renderTopStats()
		})
		defer c.sysStatsCollector.Stop()
	}

	// Stdin -> Client write loop
	return c.readStdinLoop()
}

func (c *Client) readDaemonLoop() {
	reader := protocol.NewEnvelopeReader(c.conn)
	for {
		env, err := reader.Read()
		if err != nil {
			if c.isRunning {
				c.restoreTerminal()
				c.mu.Lock()
				msg := c.disconnectMsg
				c.mu.Unlock()
				if msg != "" {
					fmt.Printf("\n[dajimux: %s]\n", msg)
				} else {
					fmt.Println("\nDisconnected from dajimux daemon.")
				}
				os.Exit(0)
			}
			return
		}

		switch env.Type {
		case protocol.MsgError:
			var errMsg string
			if err := json.Unmarshal(env.Payload, &errMsg); err == nil && errMsg != "" {
				c.mu.Lock()
				c.disconnectMsg = errMsg
				c.mu.Unlock()
			}
		case protocol.MsgFrameUpdate:
			var frame protocol.FrameUpdatePayload
			if err := json.Unmarshal(env.Payload, &frame); err == nil {
				paneID := env.PaneID
				if paneID == "" {
					paneID = frame.PaneID
				}
				rewritten := c.rewriteFrameForPane(paneID, frame.ANSI)
				if len(rewritten) > 0 {
					os.Stdout.Write(rewritten)
				}
			}
		case protocol.MsgLayoutState:
			var state protocol.LayoutStatePayload
			if err := json.Unmarshal(env.Payload, &state); err == nil {
				c.mu.Lock()
				c.lastState = state
				c.inputHandler.CurrentMode = state.Mode
				c.mu.Unlock()
				c.renderChrome(state)
			}
		case protocol.MsgPaneCaptureRes:
			var res protocol.PaneCaptureResponse
			if err := json.Unmarshal(env.Payload, &res); err == nil {
				c.mu.Lock()
				c.scrollback.SetLines(res.Lines)
				c.mu.Unlock()
				c.renderScrollback()
			}
		case protocol.MsgBell:
			os.Stdout.Write([]byte("\a"))
		}
	}
}

// rewriteFrameForPane rewrites PTY output through the ANSI compositor so cursor
// moves and text from the pane shell land strictly within that pane's viewport.
func (c *Client) rewriteFrameForPane(paneID string, ansi []byte) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()

	state := c.lastState
	cols := c.cols
	rows := c.rows

	hasBorders := len(state.Panes) > 1 && !state.IsMonocle

	// Locate target pane by ID, fallback to active pane
	var target *protocol.PaneInfo
	for i := range state.Panes {
		if state.Panes[i].ID == paneID {
			target = &state.Panes[i]
			break
		}
	}
	if target == nil {
		for i := range state.Panes {
			if state.Panes[i].IsActive {
				target = &state.Panes[i]
				break
			}
		}
	}

	// In monocle mode, do not render frames for inactive/hidden background panes
	if state.IsMonocle && target != nil && !target.IsActive {
		return nil
	}

	offX, offY, w, h := 1, 2, cols, rows-2
	targetKey := "default"
	if target != nil {
		targetKey = target.ID
		if hasBorders {
			offX = target.X + 2
			offY = target.Y + 2
			w = target.W - 2
			h = target.H - 2
		} else {
			offX = target.X + 1
			offY = target.Y + 1
			w = target.W
			h = target.H
		}
	}
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}

	if c.compositors == nil {
		c.compositors = make(map[string]*ANSICompositor)
	}

	comp, ok := c.compositors[targetKey]
	if !ok || comp == nil {
		comp = NewANSICompositor(offX, offY, w, h)
		c.compositors[targetKey] = comp
	} else {
		comp.UpdateBounds(offX, offY, w, h)
	}

	return comp.Rewrite(ansi)
}

func (c *Client) rewriteFrame(ansi []byte) []byte {
	return c.rewriteFrameForPane("", ansi)
}

func (c *Client) readStdinLoop() error {
	buf := make([]byte, 128)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		// Intercept SGR mouse events: \033[<
		if n >= 6 && buf[0] == 0x1b && buf[1] == '[' && buf[2] == '<' {
			if evt, ok := ParseSGREvent(buf[:n]); ok {
				c.handleMouseEvent(evt)
				continue
			}
		}

		// Dismiss help modal on any keypress
		if c.helpModal.IsVisible() {
			c.closeHelpModal()
			continue
		}

		res := c.inputHandler.HandleKey(buf[:n])

		c.mu.Lock()
		activePaneID := c.lastState.ActivePaneID
		c.mu.Unlock()

		// Send undo bytes if required (e.g. backspace after double-tap space)
		if len(res.UndoBytes) > 0 {
			ip, _ := json.Marshal(protocol.InputPayload{Data: res.UndoBytes})
			_ = protocol.WriteEnvelope(c.conn, protocol.Envelope{
				Type:    protocol.MsgInput,
				PaneID:  activePaneID,
				Payload: ip,
			})
		}

		// Forward raw bytes if present on non-passthrough action (e.g. double-tap leader in prefix mode)
		if len(res.RawForward) > 0 && res.Action != ActionPassThrough {
			ip, _ := json.Marshal(protocol.InputPayload{Data: res.RawForward})
			_ = protocol.WriteEnvelope(c.conn, protocol.Envelope{
				Type:    protocol.MsgInput,
				PaneID:  activePaneID,
				Payload: ip,
			})
		}

		// Notify daemon of mode return (e.g. returning to INSERT after a prefix action)
		if res.TargetMode != "" && res.Action != ActionModeChange {
			c.sendAction(protocol.ActionModeChange, map[string]string{
				"target": string(res.TargetMode),
			})
		}

		switch res.Action {
		case ActionPassThrough:
			ip, _ := json.Marshal(protocol.InputPayload{Data: res.RawForward})
			_ = protocol.WriteEnvelope(c.conn, protocol.Envelope{
				Type:    protocol.MsgInput,
				PaneID:  activePaneID,
				Payload: ip,
			})

		case ActionModeChange:
			c.sendAction(protocol.ActionModeChange, map[string]string{
				"target": string(res.TargetMode),
			})

		case ActionNavLeft:
			c.sendAction(protocol.ActionNavLeft, nil)
		case ActionNavDown:
			c.sendAction(protocol.ActionNavDown, nil)
		case ActionNavUp:
			c.sendAction(protocol.ActionNavUp, nil)
		case ActionNavRight:
			c.sendAction(protocol.ActionNavRight, nil)

		case ActionSwapNext:
			c.sendAction(protocol.ActionSwapNext, nil)
		case ActionSwapLeft:
			c.sendAction(protocol.ActionSwapLeft, nil)
		case ActionSwapDown:
			c.sendAction(protocol.ActionSwapDown, nil)
		case ActionSwapUp:
			c.sendAction(protocol.ActionSwapUp, nil)
		case ActionSwapRight:
			c.sendAction(protocol.ActionSwapRight, nil)

		case ActionMoveToWorkspace:
			c.sendAction(protocol.ActionMovePaneWS, map[string]string{
				"workspace": fmt.Sprintf("%d", res.WorkspaceNum),
			})

		case ActionSplitHorizontal:
			c.sendAction(protocol.ActionSplitH, nil)
		case ActionSplitVertical:
			c.sendAction(protocol.ActionSplitV, nil)
		case ActionSplitAuto:
			c.sendAction(protocol.ActionSplitAuto, nil)
		case ActionCycleLayout:
			c.sendAction(protocol.ActionCycleLayout, nil)
		case ActionClosePane:
			c.sendAction(protocol.ActionClosePane, nil)
		case ActionToggleMonocle:
			c.sendAction(protocol.ActionToggleMonocle, nil)
		case ActionToggleHelp:
			c.toggleHelpModal()

		case ActionSwitchWorkspace:
			c.sendAction(protocol.ActionSwitchWorkspace, map[string]string{
				"workspace": fmt.Sprintf("%d", res.WorkspaceNum),
			})

		case ActionResizeDelta:
			c.sendAction(protocol.ActionResizeDelta, map[string]string{
				"delta": fmt.Sprintf("%.2f", res.ResizeDelta),
			})

		case ActionOpenPalette:
			c.palette = NewPaletteWithWorkspaces(c.lastState.TotalWorkspaces)
			c.renderPalette()

		case ActionClosePalette:
			c.closePalette()

		case ActionPaletteType:
			if c.palette != nil && len(res.RawForward) > 0 {
				c.palette.TypeChar(rune(res.RawForward[0]))
				c.renderPalette()
			}

		case ActionPaletteBackspace:
			if c.palette != nil {
				c.palette.Backspace()
				c.renderPalette()
			}

		case ActionPaletteDown:
			if c.palette != nil {
				items := c.palette.Filter()
				c.palette.MoveDown(len(items))
				c.renderPalette()
			}

		case ActionPaletteUp:
			if c.palette != nil {
				items := c.palette.Filter()
				c.palette.MoveUp(len(items))
				c.renderPalette()
			}

		case ActionExecutePalette:
			if c.palette != nil {
				items := c.palette.Filter()
				if len(items) > 0 && c.palette.SelectedIndex < len(items) {
					selected := items[c.palette.SelectedIndex]
					c.executePaletteItem(selected)
				}
				c.closePalette()
			}

		case ActionEnterScrollback:
			c.enterScrollback()

		case ActionExitScrollback:
			c.exitScrollback()

		case ActionScrollUp:
			c.mu.Lock()
			c.scrollback.ScrollUp(1, c.rows)
			c.mu.Unlock()
			c.renderScrollback()

		case ActionScrollDown:
			c.mu.Lock()
			c.scrollback.ScrollDown(1)
			c.mu.Unlock()
			c.renderScrollback()

		case ActionScrollPageUp:
			c.mu.Lock()
			c.scrollback.ScrollUp(15, c.rows)
			c.mu.Unlock()
			c.renderScrollback()

		case ActionScrollPageDown:
			c.mu.Lock()
			c.scrollback.ScrollDown(15)
			c.mu.Unlock()
			c.renderScrollback()

		case ActionScrollTop:
			c.mu.Lock()
			c.scrollback.ScrollTop(c.rows)
			c.mu.Unlock()
			c.renderScrollback()

		case ActionScrollBottom:
			c.mu.Lock()
			c.scrollback.ScrollBottom()
			c.mu.Unlock()
			c.renderScrollback()

		case ActionYank:
			c.yankScrollback()

		case ActionDetach:
			c.detach()
			return nil

		case ActionKillSession:
			c.killSession()
			return nil

		case ActionKillServer:
			c.stopServer()
			return nil
		}
	}
}

func (c *Client) detach() {
	_ = protocol.WriteEnvelope(c.conn, protocol.Envelope{
		Type: protocol.MsgDetach,
	})
	c.mu.Lock()
	c.isRunning = false
	c.mu.Unlock()
	c.restoreTerminal()
	fmt.Printf("\n[dajimux: detached from session '%s']\n", c.SessionName)
	os.Exit(0)
}

func (c *Client) killSession() {
	payload, _ := json.Marshal(protocol.KillSessionRequest{
		SessionName: c.SessionName,
	})
	_ = protocol.WriteEnvelope(c.conn, protocol.Envelope{
		Type:    protocol.MsgKillSession,
		Payload: payload,
	})
	c.mu.Lock()
	c.isRunning = false
	c.mu.Unlock()
	c.restoreTerminal()
	fmt.Printf("\n[dajimux: session '%s' terminated]\n", c.SessionName)
	os.Exit(0)
}

func (c *Client) stopServer() {
	_ = protocol.WriteEnvelope(c.conn, protocol.Envelope{
		Type: protocol.MsgStopServer,
	})
	c.mu.Lock()
	c.isRunning = false
	c.mu.Unlock()
	c.restoreTerminal()
	fmt.Println("\n[dajimux: daemon stopped]")
	os.Exit(0)
}

func (c *Client) enterScrollback() {
	c.sendAction(protocol.ActionModeChange, map[string]string{"target": string(protocol.ModeScrollback)})
	c.mu.Lock()
	activeID := c.lastState.ActivePaneID
	c.mu.Unlock()
	req := protocol.PaneCaptureRequest{
		TargetPaneID: activeID,
		Lines:        1000,
	}
	reqData, _ := json.Marshal(req)
	_ = protocol.WriteEnvelope(c.conn, protocol.Envelope{
		Type:    protocol.MsgPaneCapture,
		Payload: reqData,
	})
}

func (c *Client) exitScrollback() {
	c.mu.Lock()
	c.scrollback.Clear()
	c.mu.Unlock()
	c.sendAction(protocol.ActionModeChange, map[string]string{"target": string(protocol.ModeNormal)})
	c.renderChrome(c.lastState)
	req := protocol.ActionPayload{Action: protocol.ActionRedraw}
	reqData, _ := json.Marshal(req)
	_ = protocol.WriteEnvelope(c.conn, protocol.Envelope{
		Type:    protocol.MsgAction,
		Payload: reqData,
	})
}

// findWorkspaceFromHeaderClick calculates which workspace tab was clicked in row 1.
func (c *Client) findWorkspaceFromHeaderClick(clickX int) int {
	c.mu.Lock()
	state := c.lastState
	inPrefix := c.inputHandler != nil && c.inputHandler.InPrefix()
	c.mu.Unlock()

	return c.chrome.WorkspaceFromHeaderClick(clickX, state, inPrefix)
}

func (c *Client) handleMouseEvent(evt *MouseEvent) {
	if evt == nil {
		return
	}

	c.mu.Lock()
	state := c.lastState
	cols := c.cols
	rows := c.rows
	c.mu.Unlock()

	// 1. Mouse Wheel Scrolling
	if evt.Button == MouseWheelUp {
		if state.Mode != protocol.ModeScrollback {
			c.enterScrollback()
		}
		c.mu.Lock()
		c.scrollback.ScrollUp(3, rows)
		c.mu.Unlock()
		c.renderScrollback()
		return
	}

	if evt.Button == MouseWheelDown {
		if state.Mode == protocol.ModeScrollback {
			c.mu.Lock()
			c.scrollback.ScrollDown(3)
			if c.scrollback.Offset() == 0 {
				c.mu.Unlock()
				c.exitScrollback()
			} else {
				c.mu.Unlock()
				c.renderScrollback()
			}
		}
		return
	}

	// 2. Mouse Drag (Dynamic Resize for both vertical and horizontal splits)
	if evt.IsDrag {
		if c.dragStart != nil && cols > 0 && rows > 0 {
			dx := evt.X - c.dragStart.X
			dy := evt.Y - c.dragStart.Y
			absDx := dx
			if absDx < 0 {
				absDx = -absDx
			}
			absDy := dy
			if absDy < 0 {
				absDy = -absDy
			}

			if absDx >= absDy && absDx > 2 {
				delta := float64(dx) / float64(cols)
				c.sendAction(protocol.ActionResizeDelta, map[string]string{
					"delta": fmt.Sprintf("%.2f", delta),
				})
				c.dragStart.X = evt.X
			} else if absDy > absDx && absDy > 1 {
				delta := float64(dy) / float64(rows)
				c.sendAction(protocol.ActionResizeDelta, map[string]string{
					"delta": fmt.Sprintf("%.2f", delta),
				})
				c.dragStart.Y = evt.Y
			}
		}
		return
	}

	// Release button terminates drag
	if !evt.IsDown {
		c.dragStart = nil
		return
	}

	// 3. Mouse Click (Left Button)
	if evt.Button == MouseBtnLeft && evt.IsDown {
		// Row 1: Top header / workspaces
		if evt.Y == 1 {
			wsNum := c.findWorkspaceFromHeaderClick(evt.X)
			if wsNum > 0 {
				c.sendAction(protocol.ActionSwitchWorkspace, map[string]string{
					"workspace": fmt.Sprintf("%d", wsNum),
				})
				return
			}
			// Clicking layout badge on the far right cycles the layout preset
			if evt.X >= cols-12 {
				c.sendAction(protocol.ActionCycleLayout, nil)
				return
			}
			return
		}

		// Rows 2..rows-1: Panes
		for _, p := range state.Panes {
			if evt.X >= p.X+1 && evt.X <= p.X+p.W && evt.Y >= p.Y+1 && evt.Y <= p.Y+p.H {
				if !p.IsActive {
					c.mu.Lock()
					c.lastState.ActivePaneID = p.ID
					for i := range c.lastState.Panes {
						c.lastState.Panes[i].IsActive = (c.lastState.Panes[i].ID == p.ID)
					}
					newState := c.lastState
					c.mu.Unlock()
					c.renderChrome(newState)
					c.sendAction(protocol.ActionFocusPane, map[string]string{
						"pane_id": p.ID,
					})
				}
				c.dragStart = &DragPoint{X: evt.X, Y: evt.Y}
				break
			}
		}
	}
}

func (c *Client) executePaletteItem(item PaletteItem) {
	switch item.Action {
	case ActionSplitAuto:
		c.sendAction(protocol.ActionSplitAuto, nil)
	case ActionSplitVertical:
		c.sendAction(protocol.ActionSplitV, nil)
	case ActionSplitHorizontal:
		c.sendAction(protocol.ActionSplitH, nil)
	case ActionSwapNext:
		c.sendAction(protocol.ActionSwapNext, nil)
	case ActionSwapLeft:
		c.sendAction(protocol.ActionSwapLeft, nil)
	case ActionSwapDown:
		c.sendAction(protocol.ActionSwapDown, nil)
	case ActionSwapUp:
		c.sendAction(protocol.ActionSwapUp, nil)
	case ActionSwapRight:
		c.sendAction(protocol.ActionSwapRight, nil)
	case ActionMoveToWorkspace:
		c.sendAction(protocol.ActionMovePaneWS, map[string]string{
			"workspace": fmt.Sprintf("%d", item.Workspace),
		})
	case ActionCycleLayout:
		c.sendAction(protocol.ActionCycleLayout, nil)
	case ActionSetLayoutBSP:
		c.sendAction(protocol.ActionSetLayout, map[string]string{"type": "bsp"})
	case ActionSetLayoutMaster:
		c.sendAction(protocol.ActionSetLayout, map[string]string{"type": "master_stack"})
	case ActionSetLayoutGrid:
		c.sendAction(protocol.ActionSetLayout, map[string]string{"type": "grid"})
	case ActionRenameWorkspace:
		query := strings.TrimSpace(c.palette.Query)
		// Extract name if query starts with "rename" or has words
		parts := strings.Fields(query)
		name := ""
		if len(parts) >= 2 && strings.EqualFold(parts[0], "rename") {
			name = strings.Join(parts[1:], " ")
		} else if len(parts) == 1 && !strings.EqualFold(parts[0], "rename") {
			name = parts[0]
		}
		if name == "" {
			name = fmt.Sprintf("work-%d", c.lastState.ActiveWorkspace)
		}
		c.sendAction(protocol.ActionRenameWorkspace, map[string]string{
			"name": name,
		})
	case ActionClosePane:
		c.sendAction(protocol.ActionClosePane, nil)
	case ActionToggleMonocle:
		c.sendAction(protocol.ActionToggleMonocle, nil)
	case ActionSwitchWorkspace:
		c.sendAction(protocol.ActionSwitchWorkspace, map[string]string{
			"workspace": fmt.Sprintf("%d", item.Workspace),
		})
	case ActionModeChange:
		c.sendAction(protocol.ActionModeChange, map[string]string{"target": string(protocol.ModeResize)})
	case ActionEnterScrollback:
		c.enterScrollback()
	case ActionToggleHelp:
		c.toggleHelpModal()
	case ActionDetach:
		c.detach()
	}
}

func (c *Client) sendAction(action string, params map[string]string) {
	ap, _ := json.Marshal(protocol.ActionPayload{
		Action: action,
		Params: params,
	})
	_ = protocol.WriteEnvelope(c.conn, protocol.Envelope{
		Type:    protocol.MsgAction,
		Payload: ap,
	})
}

// renderChrome delegates drawing of the top tab bar, pane borders, and bottom agent status bar to ChromeRenderer.
func (c *Client) renderChrome(state protocol.LayoutStatePayload) {
	c.mu.Lock()
	cols := c.cols
	rows := c.rows
	inPrefix := c.inputHandler != nil && c.inputHandler.InPrefix()
	leaderHint := c.getLeaderHint()
	compositors := c.compositors
	c.mu.Unlock()

	out, sizeChanged := c.chrome.Render(state, cols, rows, inPrefix, leaderHint, compositors)
	os.Stdout.WriteString(out)

	// If command palette is open and screen size changed, re-render it at new centered coordinates
	c.mu.Lock()
	hasPalette := c.palette != nil
	c.mu.Unlock()
	if hasPalette && sizeChanged {
		c.renderPalette()
	}
}

func (c *Client) getLeaderHint() string {
	if c.inputHandler == nil || c.inputHandler.PrefixMode != "prefix" {
		return "Space+Space: Normal Mode"
	}
	switch c.inputHandler.LeaderKey {
	case 0x02:
		return "^B: Prefix"
	case 0x01:
		return "^A: Prefix"
	case 0x00:
		return "^Space: Prefix"
	default:
		return fmt.Sprintf("%c: Prefix", c.inputHandler.LeaderKey)
	}
}



// renderTopStats writes an in-place telemetry update on Row 1 without disturbing cursor position or pane buffers.
func (c *Client) renderTopStats() {
	c.mu.Lock()
	if !c.isRunning || c.helpModal.IsVisible() || c.palette != nil || c.scrollback.Active() {
		c.mu.Unlock()
		return
	}
	cols := c.cols
	state := c.lastState
	inPrefix := c.inputHandler != nil && c.inputHandler.InPrefix()
	c.mu.Unlock()

	if out := c.chrome.RenderTopStats(state, cols, inPrefix); out != "" {
		os.Stdout.WriteString(out)
	}
}

// renderScrollback renders the scrollback buffer lines and navigation footer.
func (c *Client) renderScrollback() {
	c.mu.Lock()
	cols := c.cols
	rows := c.rows
	c.mu.Unlock()

	os.Stdout.WriteString(c.scrollback.Render(cols, rows))
}

// yankScrollback copies visible scrollback lines to the clipboard using OSC 52 and system utilities.
func (c *Client) yankScrollback() {
	c.mu.Lock()
	rows := c.rows
	text, numLines := c.scrollback.VisibleText(rows)
	c.mu.Unlock()

	if text != "" {
		copyToClipboard(text)
	}

	c.mu.Lock()
	c.scrollback.SetToast(fmt.Sprintf("✓ Copied %d lines to clipboard", numLines))
	c.mu.Unlock()
	c.renderScrollback()

	go func() {
		time.Sleep(2 * time.Second)
		c.mu.Lock()
		c.scrollback.ClearToast()
		c.mu.Unlock()
		c.renderScrollback()
	}()
}



// renderPalette draws an interactive floating overlay box centered on the terminal screen.
func (c *Client) renderPalette() {
	c.mu.Lock()
	pal := c.palette
	cols := c.cols
	rows := c.rows
	c.mu.Unlock()

	if pal == nil {
		return
	}
	os.Stdout.WriteString(c.paletteView.Render(pal, cols, rows))
}

// closePalette clears the palette frame, restores Normal mode, and requests daemon redraw.
func (c *Client) closePalette() {
	c.mu.Lock()
	c.palette = nil
	if c.inputHandler != nil {
		c.inputHandler.CurrentMode = protocol.ModeNormal
	}
	c.mu.Unlock()

	if eraseSeq, ok := c.paletteView.Clear(); ok {
		os.Stdout.WriteString(eraseSeq)
	}

	c.sendAction(protocol.ActionModeChange, map[string]string{"target": string(protocol.ModeNormal)})
	c.renderChrome(c.lastState)

	// Send redraw action to daemon to immediately repaint pane contents
	req := protocol.ActionPayload{Action: protocol.ActionRedraw}
	reqData, _ := json.Marshal(req)
	_ = protocol.WriteEnvelope(c.conn, protocol.Envelope{
		Type:    protocol.MsgAction,
		Payload: reqData,
	})
}

func (c *Client) toggleHelpModal() {
	if c.helpModal.Toggle() {
		c.renderHelpModal()
	} else {
		c.closeHelpModal()
	}
}

func (c *Client) closeHelpModal() {
	c.helpModal.Hide()
	c.mu.Lock()
	state := c.lastState
	c.mu.Unlock()

	// Clear the screen and redraw chrome and panes
	fmt.Print("\033[H\033[2J\033[r")
	c.renderChrome(state)

	// Request redraw from daemon to immediately repaint pane contents
	req := protocol.ActionPayload{Action: protocol.ActionRedraw}
	reqData, _ := json.Marshal(req)
	_ = protocol.WriteEnvelope(c.conn, protocol.Envelope{
		Type:    protocol.MsgAction,
		Payload: reqData,
	})
}

func (c *Client) renderHelpModal() {
	c.mu.Lock()
	cols := c.cols
	rows := c.rows
	c.mu.Unlock()

	os.Stdout.WriteString(c.helpModal.Render(cols, rows))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (c *Client) restoreTerminal() {
	if c.origTerm != nil {
		disableRawMode(int(os.Stdin.Fd()), c.origTerm)
		c.origTerm = nil
	}
	// Disable mouse tracking
	fmt.Print("\033[?1006l\033[?1002l\033[?1000l")
	// Reset keypad to numeric mode
	fmt.Print("\033>")
	// Reset scrolling region to full screen
	fmt.Print("\033[r")
	// Exit alternate screen buffer
	fmt.Print("\033[?1049l\033[?25h")
}

func getTermSize() (int, int, error) {
	var ws struct {
		Rows    uint16
		Cols    uint16
		Xpixels uint16
		Ypixels uint16
	}
	// Try standard descriptors in order (stdout, stdin, stderr)
	for _, f := range []*os.File{os.Stdout, os.Stdin, os.Stderr} {
		_, _, errno := syscall.Syscall(
			syscall.SYS_IOCTL,
			f.Fd(),
			syscall.TIOCGWINSZ,
			uintptr(unsafe.Pointer(&ws)),
		)
		if errno == 0 && ws.Cols > 0 && ws.Rows > 0 {
			return int(ws.Cols), int(ws.Rows), nil
		}
	}

	// Fallback to directly querying /dev/tty on Unix/Linux
	if tty, err := os.Open("/dev/tty"); err == nil {
		defer tty.Close()
		_, _, errno := syscall.Syscall(
			syscall.SYS_IOCTL,
			tty.Fd(),
			syscall.TIOCGWINSZ,
			uintptr(unsafe.Pointer(&ws)),
		)
		if errno == 0 && ws.Cols > 0 && ws.Rows > 0 {
			return int(ws.Cols), int(ws.Rows), nil
		}
	}

	return 80, 24, nil
}

func enableRawMode(fd int) (*syscall.Termios, error) {
	var oldState syscall.Termios
	if err := getTermios(fd, &oldState); err != nil {
		return nil, err
	}

	raw := oldState
	// Input modes: no break, no CR to NL, no parity check, no strip char, no start/stop output control
	raw.Iflag &^= syscall.BRKINT | syscall.ICRNL | syscall.INPCK | syscall.ISTRIP | syscall.IXON
	// Output modes: disable post-processing
	raw.Oflag &^= syscall.OPOST
	// Control modes: set 8 bit chars
	raw.Cflag |= syscall.CS8
	// Local modes: echo off, canonical off, no extended input processing, no signal chars (^Z,^C)
	raw.Lflag &^= syscall.ECHO | syscall.ICANON | syscall.IEXTEN | syscall.ISIG
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0

	if err := setTermios(fd, &raw); err != nil {
		return nil, err
	}
	return &oldState, nil
}

func disableRawMode(fd int, oldState *syscall.Termios) error {
	return setTermios(fd, oldState)
}
