package client

import (
	"encoding/base64"
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
)

// DragPoint tracks coordinate origin during mouse split dragging.
type DragPoint struct {
	X int
	Y int
}

// Client manages the terminal UI, raw input mode, and IPC connection to the daemon.
type Client struct {
	mu              sync.Mutex
	SocketPath      string
	SessionName     string
	Config          *config.Config
	conn            net.Conn
	inputHandler    *InputHandler
	palette         *Palette
	scrollbackLines []string
	scrollOffset    int
	lastState       protocol.LayoutStatePayload
	cols            int
	rows            int
	origTerm        *syscall.Termios
	isRunning       bool
	dragStart        *DragPoint
	yankToast        string
	prevPaletteX     int
	prevPaletteY     int
	prevPaletteW     int
	prevPaletteH     int
	lastRenderedCols int
	lastRenderedRows int
	compositors      map[string]*ANSICompositor
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

	return &Client{
		SocketPath:   socketPath,
		SessionName:  sessionName,
		Config:       cfg,
		inputHandler: handler,
		palette:      NewPalette(),
		compositors:  make(map[string]*ANSICompositor),
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

				// Clear terminal screen and reset margins on resize to eliminate stale borders
				os.Stdout.WriteString("\033[H\033[2J")
				if nr > 3 {
					os.Stdout.WriteString(fmt.Sprintf("\033[2;%dr", nr-1))
				}

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
				fmt.Println("\nDisconnected from dajimux daemon.")
				os.Exit(0)
			}
			return
		}

		switch env.Type {
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
				c.scrollbackLines = res.Lines
				c.scrollOffset = 0
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
			c.sendAction("mode_change", map[string]string{
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
			c.sendAction("mode_change", map[string]string{
				"target": string(res.TargetMode),
			})

		case ActionNavLeft:
			c.sendAction("nav_left", nil)
		case ActionNavDown:
			c.sendAction("nav_down", nil)
		case ActionNavUp:
			c.sendAction("nav_up", nil)
		case ActionNavRight:
			c.sendAction("nav_right", nil)

		case ActionSwapNext:
			c.sendAction("swap_next", nil)
		case ActionSwapLeft:
			c.sendAction("swap_left", nil)
		case ActionSwapDown:
			c.sendAction("swap_down", nil)
		case ActionSwapUp:
			c.sendAction("swap_up", nil)
		case ActionSwapRight:
			c.sendAction("swap_right", nil)

		case ActionMoveToWorkspace:
			c.sendAction("move_pane_ws", map[string]string{
				"workspace": fmt.Sprintf("%d", res.WorkspaceNum),
			})

		case ActionSplitHorizontal:
			c.sendAction("split_h", nil)
		case ActionSplitVertical:
			c.sendAction("split_v", nil)
		case ActionSplitAuto:
			c.sendAction("split_auto", nil)
		case ActionCycleLayout:
			c.sendAction("cycle_layout", nil)
		case ActionClosePane:
			c.sendAction("close_pane", nil)
		case ActionToggleMonocle:
			c.sendAction("toggle_monocle", nil)

		case ActionSwitchWorkspace:
			c.sendAction("switch_workspace", map[string]string{
				"workspace": fmt.Sprintf("%d", res.WorkspaceNum),
			})

		case ActionResizeDelta:
			c.sendAction("resize_delta", map[string]string{
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
			c.sendAction("mode_change", map[string]string{"target": string(protocol.ModeScrollback)})
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

		case ActionExitScrollback:
			c.exitScrollback()

		case ActionScrollUp:
			c.mu.Lock()
			maxOffset := len(c.scrollbackLines) - (c.rows - 2)
			if maxOffset < 0 {
				maxOffset = 0
			}
			if c.scrollOffset < maxOffset {
				c.scrollOffset++
			}
			c.mu.Unlock()
			c.renderScrollback()

		case ActionScrollDown:
			c.mu.Lock()
			if c.scrollOffset > 0 {
				c.scrollOffset--
			}
			c.mu.Unlock()
			c.renderScrollback()

		case ActionScrollPageUp:
			c.mu.Lock()
			maxOffset := len(c.scrollbackLines) - (c.rows - 2)
			if maxOffset < 0 {
				maxOffset = 0
			}
			c.scrollOffset += 15
			if c.scrollOffset > maxOffset {
				c.scrollOffset = maxOffset
			}
			c.mu.Unlock()
			c.renderScrollback()

		case ActionScrollPageDown:
			c.mu.Lock()
			c.scrollOffset -= 15
			if c.scrollOffset < 0 {
				c.scrollOffset = 0
			}
			c.mu.Unlock()
			c.renderScrollback()

		case ActionScrollTop:
			c.mu.Lock()
			maxOffset := len(c.scrollbackLines) - (c.rows - 2)
			if maxOffset < 0 {
				maxOffset = 0
			}
			c.scrollOffset = maxOffset
			c.mu.Unlock()
			c.renderScrollback()

		case ActionScrollBottom:
			c.mu.Lock()
			c.scrollOffset = 0
			c.mu.Unlock()
			c.renderScrollback()

		case ActionYank:
			c.yankScrollback()

		case ActionDetach:
			c.detach()
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

func (c *Client) enterScrollback() {
	c.sendAction("mode_change", map[string]string{"target": string(protocol.ModeScrollback)})
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
	c.scrollbackLines = nil
	c.scrollOffset = 0
	c.mu.Unlock()
	c.sendAction("mode_change", map[string]string{"target": string(protocol.ModeNormal)})
	c.renderChrome(c.lastState)
	req := protocol.ActionPayload{Action: "redraw"}
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
	c.mu.Unlock()

	mode := state.Mode
	if mode == "" {
		mode = protocol.ModeInsert
	}
	sessionName := state.SessionName
	if sessionName == "" {
		sessionName = "main"
	}

	// Calculate start column of workspace list in row 1:
	// Format: " MODE " + " [dajimux:SESSION] "
	startCol := 1 + (len(mode) + 2) + (len(sessionName) + 12)

	wsNames := make(map[int]string)
	for _, ws := range state.Workspaces {
		wsNames[ws.ID] = ws.Name
	}

	currCol := startCol
	totalWS := state.TotalWorkspaces
	if totalWS < 1 {
		totalWS = 1
	}
	if totalWS > 9 {
		totalWS = 9
	}

	for i := 1; i <= 9; i++ {
		name := wsNames[i]
		hasCustomName := name != "" && name != fmt.Sprintf("ws-%d", i) && (i != 1 || name != "main")

		tabLabel := fmt.Sprintf("%d", i)
		if hasCustomName {
			tabLabel = fmt.Sprintf("%d:%s", i, name)
		}

		width := len(tabLabel) + 2 // " %s "
		if i == state.ActiveWorkspace {
			width = len(tabLabel) + 3 // " %s* "
		}

		if i <= totalWS || i == state.ActiveWorkspace {
			if clickX >= currCol && clickX < currCol+width {
				return i
			}
			currCol += width
		}
	}

	return 0
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
		maxOffset := len(c.scrollbackLines) - (rows - 2)
		if maxOffset < 0 {
			maxOffset = 0
		}
		c.scrollOffset += 3
		if c.scrollOffset > maxOffset {
			c.scrollOffset = maxOffset
		}
		c.mu.Unlock()
		c.renderScrollback()
		return
	}

	if evt.Button == MouseWheelDown {
		if state.Mode == protocol.ModeScrollback {
			c.mu.Lock()
			c.scrollOffset -= 3
			if c.scrollOffset <= 0 {
				c.scrollOffset = 0
				c.mu.Unlock()
				c.exitScrollback()
			} else {
				c.mu.Unlock()
				c.renderScrollback()
			}
		}
		return
	}

	// 2. Mouse Drag (Dynamic Resize)
	if evt.IsDrag {
		if c.dragStart != nil && cols > 0 {
			dx := evt.X - c.dragStart.X
			if dx > 2 || dx < -2 {
				delta := float64(dx) / float64(cols)
				c.sendAction("resize_delta", map[string]string{
					"delta": fmt.Sprintf("%.2f", delta),
				})
				c.dragStart.X = evt.X
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
				c.sendAction("switch_workspace", map[string]string{
					"workspace": fmt.Sprintf("%d", wsNum),
				})
				return
			}
			// Clicking layout badge on the far right cycles the layout preset
			if evt.X >= cols-12 {
				c.sendAction("cycle_layout", nil)
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
					c.sendAction("focus_pane", map[string]string{
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
		c.sendAction("split_auto", nil)
	case ActionSplitVertical:
		c.sendAction("split_v", nil)
	case ActionSplitHorizontal:
		c.sendAction("split_h", nil)
	case ActionSwapNext:
		c.sendAction("swap_next", nil)
	case ActionSwapLeft:
		c.sendAction("swap_left", nil)
	case ActionSwapDown:
		c.sendAction("swap_down", nil)
	case ActionSwapUp:
		c.sendAction("swap_up", nil)
	case ActionSwapRight:
		c.sendAction("swap_right", nil)
	case ActionMoveToWorkspace:
		c.sendAction("move_pane_ws", map[string]string{
			"workspace": fmt.Sprintf("%d", item.Workspace),
		})
	case ActionCycleLayout:
		c.sendAction("cycle_layout", nil)
	case ActionSetLayoutBSP:
		c.sendAction("set_layout", map[string]string{"type": "bsp"})
	case ActionSetLayoutMaster:
		c.sendAction("set_layout", map[string]string{"type": "master_stack"})
	case ActionSetLayoutGrid:
		c.sendAction("set_layout", map[string]string{"type": "grid"})
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
		c.sendAction("rename_workspace", map[string]string{
			"name": name,
		})
	case ActionClosePane:
		c.sendAction("close_pane", nil)
	case ActionToggleMonocle:
		c.sendAction("toggle_monocle", nil)
	case ActionSwitchWorkspace:
		c.sendAction("switch_workspace", map[string]string{
			"workspace": fmt.Sprintf("%d", item.Workspace),
		})
	case ActionModeChange:
		c.sendAction("mode_change", map[string]string{"target": string(protocol.ModeResize)})
	case ActionEnterScrollback:
		c.enterScrollback()
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

// renderChrome draws the top tab bar, pane borders, and bottom agent status bar.
func (c *Client) renderChrome(state protocol.LayoutStatePayload) {
	c.mu.Lock()
	cols := c.cols
	rows := c.rows
	sizeChanged := (cols != c.lastRenderedCols || rows != c.lastRenderedRows)
	c.lastRenderedCols = cols
	c.lastRenderedRows = rows
	c.mu.Unlock()

	var b strings.Builder

	// Save cursor
	b.WriteString("\0337")

	if sizeChanged {
		// Clear entire screen and reset margins to remove stale pane borders, old footers, and overlays from previous sizes
		b.WriteString("\033[H\033[2J\033[r")
	}

	// 1. Draw Top Header (Workspaces & Mode)
	b.WriteString("\033[1;1H") // Move to row 1, col 1

	headerBg := "\033[48;5;236m"
	headerFg := "\033[38;5;255m"
	activeBorder := ""
	inactiveBorder := ""
	footerBg := "\033[48;5;236m"
	footerFg := "\033[38;5;250m"

	if c.Config != nil {
		if c.Config.Theme.StatusBarBg != "" {
			headerBg = c.Config.Theme.StatusBarBg
			footerBg = c.Config.Theme.StatusBarBg
		}
		if c.Config.Theme.StatusBarFg != "" {
			footerFg = c.Config.Theme.StatusBarFg
		}
		activeBorder = c.Config.Theme.BorderActive
		inactiveBorder = c.Config.Theme.BorderInactive
	}

	b.WriteString(headerBg)
	b.WriteString(headerFg)

	// Mode badge color
	modeText := string(state.Mode)
	modeColor := "\033[48;5;28m\033[38;5;255m" // Green for Insert
	if c.inputHandler != nil && c.inputHandler.InPrefix() {
		modeText = "PREFIX"
		modeColor = "\033[48;5;33m\033[38;5;255m\033[1m" // Bright Blue for Prefix awaiting command
	} else if state.Mode == protocol.ModeNormal {
		modeColor = "\033[48;5;166m\033[38;5;255m" // Orange for Normal
	} else if state.Mode == protocol.ModeResize {
		modeColor = "\033[48;5;160m\033[38;5;255m" // Red for Resize
	}
	// Workspaces
	wsNames := make(map[int]string)
	for _, ws := range state.Workspaces {
		wsNames[ws.ID] = ws.Name
	}

	headerPrefix := fmt.Sprintf("%s %s %s%s [dajimux:%s] ", modeColor, modeText, headerBg, headerFg, state.SessionName)
	b.WriteString(headerPrefix)
	printedHeaderLen := len(modeText) + 2 + len(state.SessionName) + 12

	for i := 1; i <= 9; i++ {
		name := wsNames[i]
		hasCustomName := name != "" && name != fmt.Sprintf("ws-%d", i) && (i != 1 || name != "main")

		tabLabel := fmt.Sprintf("%d", i)
		if hasCustomName {
			tabLabel = fmt.Sprintf("%d:%s", i, name)
		}

		if i == state.ActiveWorkspace {
			seg := fmt.Sprintf(" %s* ", tabLabel)
			b.WriteString(fmt.Sprintf("\033[48;5;240m\033[1m%s\033[22m%s", seg, headerBg))
			printedHeaderLen += len(seg)
		} else if i <= state.TotalWorkspaces {
			seg := fmt.Sprintf(" %s ", tabLabel)
			b.WriteString(seg)
			printedHeaderLen += len(seg)
		}
	}

	// Layout preset badge at far right
	layoutStr := strings.ToUpper(state.LayoutType)
	if layoutStr == "" || layoutStr == "BSP" {
		layoutStr = "BSP"
	} else if layoutStr == "MASTER_STACK" {
		layoutStr = "MASTER"
	} else if layoutStr == "GRID" {
		layoutStr = "GRID"
	}
	layoutBadge := fmt.Sprintf(" <%s> ", layoutStr)

	// Pad header and place layout indicator at far right
	if cols > printedHeaderLen+len(layoutBadge) {
		pad := cols - printedHeaderLen - len(layoutBadge)
		b.WriteString(strings.Repeat(" ", pad))
		b.WriteString("\033[1m\033[38;5;51m" + layoutBadge + "\033[0m" + headerBg + headerFg)
	} else if cols > printedHeaderLen {
		b.WriteString(strings.Repeat(" ", cols-printedHeaderLen))
	}
	b.WriteString("\033[0m")

	// 2. Draw Borders and Pane Headers
	b.WriteString(RenderBordersWithTheme(state.Panes, cols, rows, state.IsMonocle, activeBorder, inactiveBorder))

	// 3. Draw Bottom Status Bar (Agent Telemetry & Shortcuts)
	b.WriteString(fmt.Sprintf("\033[%d;1H", rows)) // Move to last row
	b.WriteString(footerBg)
	b.WriteString(footerFg)

	// Look for active pane's agent telemetry
	var agentTelemetry string
	for _, p := range state.Panes {
		if p.IsActive && p.AgentBadge != "" {
			agentTelemetry = fmt.Sprintf(" | Agent: %s", p.AgentBadge)
			break
		}
	}

	leaderHint := "Space+Space: Normal"
	if c.inputHandler != nil && c.inputHandler.PrefixMode == "prefix" {
		switch c.inputHandler.LeaderKey {
		case 0x02:
			leaderHint = "^B: Prefix"
		case 0x01:
			leaderHint = "^A: Prefix"
		case 0x00:
			leaderHint = "^Space: Prefix"
		default:
			leaderHint = fmt.Sprintf("%c: Prefix", c.inputHandler.LeaderKey)
		}
	}

	footerText := fmt.Sprintf(" %s | h/j/k/l: Nav | w: Swap | t: Layout | s/v/e: Split | m<1-9>: Move WS | x: Close%s", leaderHint, agentTelemetry)
	b.WriteString(footerText)
	if cols > len(footerText) {
		b.WriteString(strings.Repeat(" ", cols-len(footerText)))
	}
	b.WriteString("\033[0m")

	// Position cursor inside active pane viewport
	activeY := 2
	activeX := 1
	hasBorders := len(state.Panes) > 1 && !state.IsMonocle
	for _, p := range state.Panes {
		if p.IsActive {
			if hasBorders {
				activeY = p.Y + 2
				activeX = p.X + 2
			} else {
				activeY = p.Y + 1
				activeX = p.X + 1
			}
			break
		}
	}
	b.WriteString(fmt.Sprintf("\033[%d;%dH", activeY, activeX))

	os.Stdout.WriteString(b.String())

	// If command palette is open and screen size changed, re-render it at new centered coordinates
	if c.palette != nil && sizeChanged {
		c.renderPalette()
	}
}

// renderScrollback renders the scrollback buffer lines and navigation footer.
func (c *Client) renderScrollback() {
	c.mu.Lock()
	lines := c.scrollbackLines
	offset := c.scrollOffset
	cols := c.cols
	rows := c.rows
	c.mu.Unlock()

	var b strings.Builder
	b.WriteString("\0337") // Save cursor

	viewHeight := rows - 2
	if viewHeight < 1 {
		viewHeight = 1
	}

	totalLines := len(lines)
	endIdx := totalLines - offset
	startIdx := endIdx - viewHeight
	if startIdx < 0 {
		startIdx = 0
	}
	if endIdx < 0 {
		endIdx = 0
	}
	if endIdx > totalLines {
		endIdx = totalLines
	}

	// Render lines in the viewport
	row := 2
	for i := startIdx; i < endIdx && row < rows; i++ {
		line := lines[i]
		if len(line) > cols {
			line = line[:cols]
		}
		b.WriteString(fmt.Sprintf("\033[%d;1H\033[2K%s", row, line))
		row++
	}
	// Clear any remaining lines in viewport
	for ; row < rows; row++ {
		b.WriteString(fmt.Sprintf("\033[%d;1H\033[2K", row))
	}

	// Footer banner
	bannerBg := "\033[48;5;166m\033[38;5;255m" // Orange banner default
	status := fmt.Sprintf(" -- SCROLLBACK [%d/%d] -- (k/j: line, ^u/^d: page, g/G: top/bottom, y: yank, q: exit) ",
		totalLines-offset, totalLines)
	if c.yankToast != "" {
		bannerBg = "\033[48;5;28m\033[38;5;255m\033[1m" // Bright green banner for toast confirmation
		status = fmt.Sprintf(" %s ", c.yankToast)
	}

	b.WriteString(fmt.Sprintf("\033[%d;1H%s", rows, bannerBg))
	b.WriteString(status)
	if cols > len(status) {
		b.WriteString(strings.Repeat(" ", cols-len(status)))
	}
	b.WriteString("\033[0m")

	b.WriteString("\0338") // Restore cursor
	os.Stdout.WriteString(b.String())
}

// yankScrollback copies visible scrollback lines to the clipboard using OSC 52.
func (c *Client) yankScrollback() {
	c.mu.Lock()
	lines := c.scrollbackLines
	offset := c.scrollOffset
	rows := c.rows
	c.mu.Unlock()

	viewHeight := rows - 2
	totalLines := len(lines)
	endIdx := totalLines - offset
	startIdx := endIdx - viewHeight
	if startIdx < 0 {
		startIdx = 0
	}
	if endIdx > totalLines {
		endIdx = totalLines
	}

	var visibleText string
	numLines := 0
	if startIdx < endIdx {
		numLines = endIdx - startIdx
		visibleText = strings.Join(lines[startIdx:endIdx], "\n")
	}

	if visibleText != "" {
		b64 := base64.StdEncoding.EncodeToString([]byte(visibleText))
		fmt.Printf("\033]52;c;%s\a", b64)
	}

	c.mu.Lock()
	c.yankToast = fmt.Sprintf("✓ Copied %d lines to clipboard (OSC 52)", numLines)
	c.mu.Unlock()
	c.renderScrollback()

	go func() {
		time.Sleep(2 * time.Second)
		c.mu.Lock()
		c.yankToast = ""
		c.mu.Unlock()
		c.renderScrollback()
	}()
}

// renderPalette draws an interactive floating overlay box centered on the terminal screen.
func (c *Client) renderPalette() {
	if c.palette == nil {
		return
	}

	c.mu.Lock()
	cols := c.cols
	rows := c.rows
	c.mu.Unlock()

	items := c.palette.Filter()
	boxWidth := 72
	if boxWidth > cols-4 {
		boxWidth = cols - 4
	}
	if boxWidth < 30 {
		boxWidth = 30
	}

	maxVisibleItems := 8
	boxHeight := maxVisibleItems + 4

	startX := (cols - boxWidth) / 2
	startY := (rows - boxHeight) / 2
	if startX < 1 {
		startX = 1
	}
	if startY < 2 {
		startY = 2
	}

	var b strings.Builder
	b.WriteString("\0337") // Save cursor

	// If the bounding box moved or changed size, clear the old bounding area first
	if c.prevPaletteH > 0 && (c.prevPaletteX != startX || c.prevPaletteY != startY || c.prevPaletteW != boxWidth || c.prevPaletteH != boxHeight) {
		for y := c.prevPaletteY; y < c.prevPaletteY+c.prevPaletteH; y++ {
			b.WriteString(fmt.Sprintf("\033[%d;%dH%s", y, c.prevPaletteX, strings.Repeat(" ", c.prevPaletteW)))
		}
	}
	c.prevPaletteX = startX
	c.prevPaletteY = startY
	c.prevPaletteW = boxWidth
	c.prevPaletteH = boxHeight

	// Palette Border & Colors: Deep Blue background (237) and Cyan borders (38;5;51)
	bg := "\033[48;5;235m"
	fg := "\033[38;5;255m"
	borderFg := "\033[38;5;51m"
	reset := "\033[0m"

	// Adjust scroll window so SelectedIndex is always visible
	c.palette.AdjustScroll(maxVisibleItems, len(items))

	// 1. Top border with title and item count
	title := " Command Palette "
	countBadge := ""
	if len(items) > 0 {
		countBadge = fmt.Sprintf(" [%d/%d] ", c.palette.SelectedIndex+1, len(items))
	}
	dashes := boxWidth - 2 - len(title) - len(countBadge)
	if dashes < 0 {
		dashes = 0
	}
	b.WriteString(fmt.Sprintf("\033[%d;%dH%s%s┌─%s%s%s%s%s%s┐%s",
		startY, startX, bg, borderFg, fg, title, borderFg, strings.Repeat("─", dashes), fg, countBadge, reset))

	// 2. Query input line
	queryPrompt := fmt.Sprintf("> %s_", c.palette.Query)
	padQuery := boxWidth - 4 - len(queryPrompt)
	if padQuery < 0 {
		padQuery = 0
	}
	b.WriteString(fmt.Sprintf("\033[%d;%dH%s%s│ %s\033[1m%s\033[0m%s%s%s│%s",
		startY+1, startX, bg, borderFg, fg, queryPrompt, bg, strings.Repeat(" ", padQuery), borderFg, reset))

	// 3. Separator with scroll-up indicator
	scrollUpInd := "─"
	if c.palette.ScrollOffset > 0 {
		scrollUpInd = " ▲ more above ─"
	}
	sepDashes := boxWidth - 2 - len(scrollUpInd)
	if sepDashes < 0 {
		sepDashes = 0
	}
	b.WriteString(fmt.Sprintf("\033[%d;%dH%s%s├%s%s┤%s",
		startY+2, startX, bg, borderFg, scrollUpInd, strings.Repeat("─", sepDashes), reset))

	// 4. Fixed height scrolling item slots
	for i := 0; i < maxVisibleItems; i++ {
		slotY := startY + 3 + i
		itemIdx := c.palette.ScrollOffset + i
		if itemIdx < len(items) {
			item := items[itemIdx]
			prefix := "  "
			lineBg := bg
			lineFg := fg
			if itemIdx == c.palette.SelectedIndex {
				prefix = "> "
				lineBg = "\033[48;5;24m" // Highlighted dark cyan
				lineFg = "\033[1m\033[38;5;255m"
			}

			lineText := fmt.Sprintf("%s%-22s %s", prefix, item.Title, item.Description)
			if len(lineText) > boxWidth-4 {
				lineText = lineText[:boxWidth-4]
			}
			padItem := boxWidth - 2 - len(lineText)
			if padItem < 0 {
				padItem = 0
			}

			b.WriteString(fmt.Sprintf("\033[%d;%dH%s%s│%s%s%s%s%s│%s",
				slotY, startX, borderFg, lineBg, lineFg, lineText, strings.Repeat(" ", padItem), reset, borderFg, reset))
		} else {
			// Clean background padding for empty slot
			padEmpty := boxWidth - 2
			b.WriteString(fmt.Sprintf("\033[%d;%dH%s%s│%s%s│%s",
				slotY, startX, borderFg, bg, strings.Repeat(" ", padEmpty), borderFg, reset))
		}
	}

	// 5. Bottom border with scroll-down indicator & keybinding hint
	bottomY := startY + 3 + maxVisibleItems
	scrollDownInd := ""
	if c.palette.ScrollOffset+maxVisibleItems < len(items) {
		scrollDownInd = "▼ "
	}
	hint := fmt.Sprintf(" %s[Esc] Close │ [Enter] Run │ [↑/↓] Select ", scrollDownInd)
	hintDashes := boxWidth - 2 - len(hint)
	if hintDashes < 0 {
		hintDashes = 0
	}
	b.WriteString(fmt.Sprintf("\033[%d;%dH%s%s└─%s%s%s%s┘%s",
		bottomY, startX, bg, borderFg, fg, hint, borderFg, strings.Repeat("─", hintDashes), reset))

	b.WriteString("\0338") // Restore cursor
	os.Stdout.WriteString(b.String())
}

// closePalette clears the palette frame, restores Normal mode, and requests daemon redraw.
func (c *Client) closePalette() {
	c.mu.Lock()
	pX, pY, pW, pH := c.prevPaletteX, c.prevPaletteY, c.prevPaletteW, c.prevPaletteH
	c.prevPaletteX, c.prevPaletteY, c.prevPaletteW, c.prevPaletteH = 0, 0, 0, 0
	c.palette = nil
	if c.inputHandler != nil {
		c.inputHandler.CurrentMode = protocol.ModeNormal
	}
	c.mu.Unlock()

	// Erase the palette bounding box on screen
	if pH > 0 && pW > 0 {
		var b strings.Builder
		b.WriteString("\0337") // Save cursor
		for y := pY; y <= pY+pH; y++ {
			b.WriteString(fmt.Sprintf("\033[%d;%dH%s", y, pX, strings.Repeat(" ", pW)))
		}
		b.WriteString("\0338") // Restore cursor
		os.Stdout.WriteString(b.String())
	}

	c.sendAction("mode_change", map[string]string{"target": string(protocol.ModeNormal)})
	c.renderChrome(c.lastState)

	// Send redraw action to daemon to immediately repaint pane contents
	req := protocol.ActionPayload{Action: "redraw"}
	reqData, _ := json.Marshal(req)
	_ = protocol.WriteEnvelope(c.conn, protocol.Envelope{
		Type:    protocol.MsgAction,
		Payload: reqData,
	})
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
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		os.Stdout.Fd(),
		syscall.TIOCGWINSZ,
		uintptr(unsafe.Pointer(&ws)),
	)
	if errno != 0 {
		return 80, 24, errno
	}
	return int(ws.Cols), int(ws.Rows), nil
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
