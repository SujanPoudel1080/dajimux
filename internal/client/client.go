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
	"unsafe"

	"github.com/SujanPoudel1080/dajimux/internal/protocol"
)

// Client manages the terminal UI, raw input mode, and IPC connection to the daemon.
type Client struct {
	mu              sync.Mutex
	SocketPath      string
	SessionName     string
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
}

// NewClient initializes a client for the target session.
func NewClient(socketPath, sessionName string) *Client {
	if sessionName == "" {
		sessionName = "main"
	}
	return &Client{
		SocketPath:   socketPath,
		SessionName:  sessionName,
		inputHandler: NewInputHandler(),
		palette:      NewPalette(),
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

	// Clear screen and enter alternate screen buffer
	fmt.Print("\033[?1049h\033[H\033[2J")
	if c.rows > 3 {
		fmt.Printf("\033[2;%dr", c.rows-1)
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
				rewritten := c.rewriteFrame(frame.ANSI)
				os.Stdout.Write(rewritten)
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
		}
	}
}

// rewriteFrame rewrites PTY output through the ANSI compositor so cursor
// moves from the pane shell land inside the pane's viewport on the screen.
func (c *Client) rewriteFrame(ansi []byte) []byte {
	c.mu.Lock()
	state := c.lastState
	rows := c.rows
	c.mu.Unlock()

	// Find active pane bounds
	offX, offY, w, h := 1, 2, 80, rows-2
	for _, p := range state.Panes {
		if p.IsActive {
			offX = p.X + 1 // BSP rect is 0-based, terminal is 1-based
			offY = p.Y + 1 // p.Y already starts at 1 (row 0 is header), so +1 gives 1-based terminal row
			w = p.W
			h = p.H
			break
		}
	}
	if h < 1 {
		h = 1
	}

	comp := NewANSICompositor(offX, offY, w, h)
	return comp.Rewrite(ansi)
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

		res := c.inputHandler.HandleKey(buf[:n])

		// Send undo bytes if required (e.g. backspace after double-tap space)
		if len(res.UndoBytes) > 0 {
			ip, _ := json.Marshal(protocol.InputPayload{Data: res.UndoBytes})
			_ = protocol.WriteEnvelope(c.conn, protocol.Envelope{
				Type:    protocol.MsgInput,
				Payload: ip,
			})
		}

		switch res.Action {
		case ActionPassThrough:
			ip, _ := json.Marshal(protocol.InputPayload{Data: res.RawForward})
			_ = protocol.WriteEnvelope(c.conn, protocol.Envelope{
				Type:    protocol.MsgInput,
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

		case ActionSplitHorizontal:
			c.sendAction("split_h", nil)
		case ActionSplitVertical:
			c.sendAction("split_v", nil)
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
			c.palette = nil
			c.sendAction("mode_change", map[string]string{"target": string(protocol.ModeNormal)})
			c.renderChrome(c.lastState)

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
				c.palette.MoveUp()
				c.renderPalette()
			}

		case ActionExecutePalette:
			if c.palette != nil {
				items := c.palette.Filter()
				if len(items) > 0 && c.palette.SelectedIndex < len(items) {
					selected := items[c.palette.SelectedIndex]
					c.executePaletteItem(selected)
				}
				c.palette = nil
				c.sendAction("mode_change", map[string]string{"target": string(protocol.ModeNormal)})
				c.renderChrome(c.lastState)
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

func (c *Client) executePaletteItem(item PaletteItem) {
	switch item.Action {
	case ActionSplitVertical:
		c.sendAction("split_v", nil)
	case ActionSplitHorizontal:
		c.sendAction("split_h", nil)
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
	c.mu.Unlock()

	var b strings.Builder

	// Save cursor
	b.WriteString("\0337")

	// 1. Draw Top Header (Workspaces & Mode)
	b.WriteString("\033[1;1H") // Move to row 1, col 1
	b.WriteString("\033[48;5;236m\033[38;5;255m") // Dark gray bg, white fg

	// Mode badge color
	modeColor := "\033[48;5;28m\033[38;5;255m" // Green for Insert
	if state.Mode == protocol.ModeNormal {
		modeColor = "\033[48;5;166m\033[38;5;255m" // Orange for Normal
	} else if state.Mode == protocol.ModeResize {
		modeColor = "\033[48;5;160m\033[38;5;255m" // Red for Resize
	}
	b.WriteString(fmt.Sprintf("%s %s \033[48;5;236m\033[38;5;255m", modeColor, state.Mode))

	// Workspaces
	b.WriteString(fmt.Sprintf(" [dajimux:%s] ", state.SessionName))
	for i := 1; i <= 9; i++ {
		if i == state.ActiveWorkspace {
			b.WriteString(fmt.Sprintf("\033[48;5;240m\033[1m %d* \033[22m\033[48;5;236m", i))
		} else if i <= state.TotalWorkspaces {
			b.WriteString(fmt.Sprintf(" %d ", i))
		}
	}

	// Pad header to end of line
	headerLen := 30 + (state.TotalWorkspaces * 4)
	if cols > headerLen {
		b.WriteString(strings.Repeat(" ", cols-headerLen))
	}
	b.WriteString("\033[0m")

	// 2. Draw Borders and Pane Headers
	b.WriteString(RenderBorders(state.Panes, cols, rows, state.IsMonocle))

	// 3. Draw Bottom Status Bar (Agent Telemetry & Shortcuts)
	b.WriteString(fmt.Sprintf("\033[%d;1H", rows)) // Move to last row
	b.WriteString("\033[48;5;236m\033[38;5;250m")

	// Look for active pane's agent telemetry
	var agentTelemetry string
	for _, p := range state.Panes {
		if p.IsActive && p.AgentBadge != "" {
			agentTelemetry = fmt.Sprintf(" | Agent: %s", p.AgentBadge)
			break
		}
	}

	footerText := fmt.Sprintf(" Space+Space: Normal Mode | h/j/k/l: Nav | s/v: Split | x: Close | 1-9: WS%s", agentTelemetry)
	b.WriteString(footerText)
	if cols > len(footerText) {
		b.WriteString(strings.Repeat(" ", cols-len(footerText)))
	}
	b.WriteString("\033[0m")

	// Position cursor inside active pane viewport
	activeY := 2
	activeX := 1
	for _, p := range state.Panes {
		if p.IsActive {
			activeY = p.Y + 1
			activeX = p.X + 1
			break
		}
	}
	b.WriteString(fmt.Sprintf("\033[%d;%dH", activeY, activeX))

	os.Stdout.WriteString(b.String())
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
	b.WriteString(fmt.Sprintf("\033[%d;1H\033[48;5;166m\033[38;5;255m", rows)) // Orange banner
	status := fmt.Sprintf(" -- SCROLLBACK [%d/%d] -- (k/j: line, ^u/^d: page, g/G: top/bottom, y: yank, q: exit) ",
		totalLines-offset, totalLines)
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
	if startIdx < endIdx {
		visibleText = strings.Join(lines[startIdx:endIdx], "\n")
	}

	if visibleText != "" {
		b64 := base64.StdEncoding.EncodeToString([]byte(visibleText))
		fmt.Printf("\033]52;c;%s\a", b64)
	}
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
	boxHeight := len(items) + 4
	if boxHeight > maxVisibleItems+4 {
		boxHeight = maxVisibleItems + 4
	}

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

	// Palette Border & Colors: Deep Blue background (237) and Cyan borders (38;5;51)
	bg := "\033[48;5;235m"
	fg := "\033[38;5;255m"
	borderFg := "\033[38;5;51m"
	reset := "\033[0m"

	// 1. Top border
	title := " Command Palette "
	dashes := boxWidth - 2 - len(title)
	if dashes < 0 {
		dashes = 0
	}
	b.WriteString(fmt.Sprintf("\033[%d;%dH%s%s┌─%s%s%s%s┐%s",
		startY, startX, bg, borderFg, fg, title, borderFg, strings.Repeat("─", dashes), reset))

	// 2. Query input line
	queryPrompt := fmt.Sprintf("> %s_", c.palette.Query)
	padQuery := boxWidth - 4 - len(queryPrompt)
	if padQuery < 0 {
		padQuery = 0
	}
	b.WriteString(fmt.Sprintf("\033[%d;%dH%s%s│ %s\033[1m%s\033[0m%s%s%s│%s",
		startY+1, startX, bg, borderFg, fg, queryPrompt, bg, strings.Repeat(" ", padQuery), borderFg, reset))

	// 3. Separator
	b.WriteString(fmt.Sprintf("\033[%d;%dH%s%s├%s┤%s",
		startY+2, startX, bg, borderFg, strings.Repeat("─", boxWidth-2), reset))

	// 4. Items
	for i := 0; i < maxVisibleItems && i < len(items); i++ {
		item := items[i]
		prefix := "  "
		lineBg := bg
		lineFg := fg
		if i == c.palette.SelectedIndex {
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
			startY+3+i, startX, borderFg, lineBg, lineFg, lineText, strings.Repeat(" ", padItem), reset, borderFg, reset))
	}

	// 5. Bottom border
	bottomY := startY + 3 + min(maxVisibleItems, len(items))
	b.WriteString(fmt.Sprintf("\033[%d;%dH%s%s└%s┘%s",
		bottomY, startX, bg, borderFg, strings.Repeat("─", boxWidth-2), reset))

	b.WriteString("\0338") // Restore cursor
	os.Stdout.WriteString(b.String())
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
