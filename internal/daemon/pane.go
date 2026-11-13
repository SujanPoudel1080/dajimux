package daemon

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"

	"github.com/SujanPoudel1080/dajimux/internal/bsp"
	"github.com/creack/pty"
)

// Pane represents a single virtual terminal window running a shell or program.
type Pane struct {
	mu          sync.RWMutex
	ID          string
	Title       string
	Cwd         string
	PTY         *os.File
	Cmd         *exec.Cmd
	Bounds      bsp.Rect
	AgentState  string
	AgentBadge  string
	rawBuffer   []byte
	maxRawBytes int
	scrollback  []string
	maxLines    int
	isClosed    bool
	listeners   map[chan []byte]struct{}
	onExit      func(paneID string)
}

// NewPane allocates a pane instance with default 2000 lines scrollback buffer.
func NewPane(id, title string) *Pane {
	return NewPaneWithConfig(id, title, 2000)
}

// NewPaneWithConfig allocates a pane instance with custom scrollback history limit.
func NewPaneWithConfig(id, title string, maxLines int) *Pane {
	if maxLines <= 0 {
		maxLines = 2000
	}
	return &Pane{
		ID:          id,
		Title:       title,
		maxRawBytes: 256 * 1024, // 256KB raw ANSI history for replay on attach
		rawBuffer:   make([]byte, 0, 8192),
		maxLines:    maxLines,
		scrollback:  make([]string, 0, 500),
		listeners:   make(map[chan []byte]struct{}),
	}
}

// SetOnExit sets a callback to execute when the child process terminates.
func (p *Pane) SetOnExit(fn func(paneID string)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onExit = fn
}

// Start spawns an interactive login shell inside a new pseudoterminal.
func (p *Pane) Start(shellPath string, cols, rows int, env []string) error {
	return p.StartWithCwd(shellPath, cols, rows, env, "")
}

// StartWithCwd spawns an interactive login shell inside a pseudoterminal with working directory cwd.
func (p *Pane) StartWithCwd(shellPath string, cols, rows int, env []string, cwd string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	shellPath = resolveDefaultShell(shellPath)

	if cols < 2 {
		cols = 80
	}
	if rows < 2 {
		rows = 24
	}

	p.Bounds = bsp.Rect{W: cols, H: rows}

	if cwd != "" {
		p.Cwd = cwd
	} else if p.Cwd == "" {
		wd, err := os.Getwd()
		if err == nil {
			p.Cwd = wd
		}
	}

	// Launch as interactive login shell
	cmd := exec.Command(shellPath, "-l")
	if p.Cwd != "" {
		cmd.Dir = p.Cwd
	}
	cmd.Env = append(os.Environ(), env...)
	cmd.Env = append(cmd.Env,
		fmt.Sprintf("DAJIMUX_PANE_ID=%s", p.ID),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
	)

	// Ensure PATH contains standard executable locations on Linux & BSD/macOS
	hasPath := false
	for _, e := range cmd.Env {
		if strings.HasPrefix(e, "PATH=") {
			hasPath = true
			break
		}
	}
	if !hasPath {
		home := os.Getenv("HOME")
		defaultPath := "/usr/local/bin:/usr/bin:/bin:/usr/local/sbin:/usr/sbin:/sbin"
		if home != "" {
			defaultPath = fmt.Sprintf("%s:%s/.local/bin:%s/bin", defaultPath, home, home)
		}
		cmd.Env = append(cmd.Env, "PATH="+defaultPath)
	}

	// Start with PTY sizing and controlling terminal setup via creack/pty
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	})
	if err != nil {
		return fmt.Errorf("pty.StartWithSize: %w", err)
	}

	p.PTY = ptmx
	p.Cmd = cmd

	// Stream PTY output into raw replay buffer and registered listeners
	go p.readLoop()

	// Supervise process exit
	go func() {
		_ = cmd.Wait()
		p.Close()
		p.mu.RLock()
		exitCb := p.onExit
		p.mu.RUnlock()
		if exitCb != nil {
			exitCb(p.ID)
		}
	}()

	return nil
}

// readLoop reads raw stdout from master PTY and broadcasts to listeners.
func (p *Pane) readLoop() {
	buf := make([]byte, 4096)
	lineBuf := strings.Builder{}

	for {
		p.mu.RLock()
		closed := p.isClosed
		ptmx := p.PTY
		p.mu.RUnlock()
		if closed || ptmx == nil {
			break
		}

		n, err := ptmx.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])

			p.mu.Lock()
			// Append to raw ANSI replay buffer
			p.rawBuffer = append(p.rawBuffer, chunk...)
			if len(p.rawBuffer) > p.maxRawBytes {
				p.rawBuffer = p.rawBuffer[len(p.rawBuffer)-p.maxRawBytes:]
			}

			// Broadcast to all active attached client listeners
			for ch := range p.listeners {
				select {
				case ch <- chunk:
				default:
				}
			}
			p.mu.Unlock()

			// Parse clean lines for agent capture / scrollback
			for _, b := range chunk {
				if b == '\n' {
					p.appendScrollback(lineBuf.String())
					lineBuf.Reset()
				} else if b != '\r' {
					lineBuf.WriteByte(b)
				}
			}
		}

		if err != nil {
			if err != io.EOF {
				// Master closed or child exited
			}
			break
		}
	}
}

// AddListener registers a channel to receive real-time PTY chunks.
func (p *Pane) AddListener(ch chan []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.listeners[ch] = struct{}{}
}

// RemoveListener unregisters a channel.
func (p *Pane) RemoveListener(ch chan []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.listeners, ch)
}

// GetRawBuffer returns a copy of the recent ANSI output history.
func (p *Pane) GetRawBuffer() []byte {
	p.mu.RLock()
	defer p.mu.RUnlock()
	res := make([]byte, len(p.rawBuffer))
	copy(res, p.rawBuffer)
	return res
}

func (p *Pane) appendScrollback(line string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.scrollback) >= p.maxLines {
		p.scrollback = p.scrollback[1:]
	}
	p.scrollback = append(p.scrollback, line)
}

// CaptureLastLines returns the last n lines for agent inspection.
func (p *Pane) CaptureLastLines(n int) []string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	total := len(p.scrollback)
	if total == 0 {
		return nil
	}
	if n > total || n <= 0 {
		n = total
	}
	res := make([]string, n)
	copy(res, p.scrollback[total-n:])
	return res
}

// Write writes raw bytes directly to the PTY master.
func (p *Pane) Write(data []byte) (int, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.isClosed || p.PTY == nil {
		return 0, fmt.Errorf("pane %s is closed", p.ID)
	}
	return p.PTY.Write(data)
}

// Resize updates the terminal dimensions and sends SIGWINCH to the child process.
func (p *Pane) Resize(cols, rows int) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if cols < 2 {
		cols = 2
	}
	if rows < 2 {
		rows = 2
	}

	p.Bounds.W = cols
	p.Bounds.H = rows

	if p.isClosed || p.PTY == nil {
		return nil
	}
	return pty.Setsize(p.PTY, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	})
}

// Close terminates the pane process and releases PTY fds.
func (p *Pane) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.isClosed {
		return nil
	}
	p.isClosed = true

	if p.Cmd != nil && p.Cmd.Process != nil && p.Cmd.Process.Pid > 0 {
		pid := p.Cmd.Process.Pid
		// Terminate process group if available (especially on Linux)
		_ = syscall.Kill(-pid, syscall.SIGHUP)
		_ = p.Cmd.Process.Signal(syscall.SIGHUP)
	}
	if p.PTY != nil {
		_ = p.PTY.Close()
	}
	return nil
}

// resolveDefaultShell determines the best available shell across Linux, macOS, and BSD systems.
func resolveDefaultShell(requested string) string {
	if requested != "" {
		if path, err := exec.LookPath(requested); err == nil {
			return path
		}
		if _, err := os.Stat(requested); err == nil {
			return requested
		}
	}

	if env := os.Getenv("SHELL"); env != "" {
		if path, err := exec.LookPath(env); err == nil {
			return path
		}
		if _, err := os.Stat(env); err == nil {
			return env
		}
	}

	// Try standard login shells in order of preference
	for _, name := range []string{"bash", "zsh", "sh"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}

	systemPaths := []string{
		"/bin/bash",
		"/usr/bin/bash",
		"/bin/sh",
		"/usr/bin/sh",
		"/bin/zsh",
		"/usr/bin/zsh",
	}
	for _, path := range systemPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return "/bin/sh"
}
