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

	"github.com/SujanPoudel1080/dajimux/internal/protocol"
)

// Client manages the terminal UI, raw input mode, and IPC connection to the daemon.
type Client struct {
	mu           sync.Mutex
	SocketPath   string
	SessionName  string
	conn         net.Conn
	inputHandler *InputHandler
	lastState    protocol.LayoutStatePayload
	cols         int
	rows         int
	origTerm     *syscall.Termios
	isRunning    bool
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
	}
}

// Run attaches to the daemon and enters the interactive raw terminal loop.
func (c *Client) Run() error {
	conn, err := net.Dial("unix", c.SocketPath)
	if err != nil {
		return fmt.Errorf("connect to daemon at %s: %w", c.SocketPath, err)
	}
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
	for {
		env, err := protocol.ReadEnvelope(c.conn)
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
				os.Stdout.Write(frame.ANSI)
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
		}
	}
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
		}
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

	// 2. Draw Bottom Status Bar (Agent Telemetry & Shortcuts)
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

	// Restore cursor
	b.WriteString("\0338")

	os.Stdout.WriteString(b.String())
}

func (c *Client) restoreTerminal() {
	if c.origTerm != nil {
		disableRawMode(int(os.Stdin.Fd()), c.origTerm)
		c.origTerm = nil
	}
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
		uintptr(unsafePointer(&ws)),
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
