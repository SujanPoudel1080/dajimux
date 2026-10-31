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
)

// Pane represents a single virtual terminal window running a shell or program.
type Pane struct {
	mu          sync.RWMutex
	ID          string
	Title       string
	PTY         *os.File
	Cmd         *exec.Cmd
	Bounds      bsp.Rect
	AgentState  string
	AgentBadge  string
	scrollback  []string
	maxLines    int
	isClosed    bool
	OutputCh    chan []byte
}

// NewPane allocates a pane instance with in-memory scrollback buffer.
func NewPane(id, title string) *Pane {
	return &Pane{
		ID:         id,
		Title:      title,
		maxLines:   2000,
		scrollback: make([]string, 0, 500),
		OutputCh:   make(chan []byte, 128),
	}
}

// Start spawns a shell process inside a new pseudoterminal.
func (p *Pane) Start(shellPath string, cols, rows int, env []string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if shellPath == "" {
		shellPath = os.Getenv("SHELL")
		if shellPath == "" {
			shellPath = "/bin/zsh"
		}
	}

	p.Bounds = bsp.Rect{W: cols, H: rows}

	// Prepare command
	cmd := exec.Command(shellPath)
	cmd.Env = append(os.Environ(), env...)
	cmd.Env = append(cmd.Env,
		fmt.Sprintf("DAJIMUX_PANE_ID=%s", p.ID),
		fmt.Sprintf("TERM=%s", "xterm-256color"),
	)

	// Open pseudoterminal master & slave
	master, slave, err := openPTY()
	if err != nil {
		return fmt.Errorf("openpty: %w", err)
	}

	p.PTY = master
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	if err := cmd.Start(); err != nil {
		master.Close()
		slave.Close()
		return fmt.Errorf("cmd.Start: %w", err)
	}

	// Slave fd is owned by child process now; close in parent
	slave.Close()
	p.Cmd = cmd

	// Set initial window size
	p.setWindowSize(cols, rows)

	// Stream PTY output into scrollback buffer and OutputCh
	go p.readLoop()

	// Supervise process exit
	go func() {
		_ = cmd.Wait()
		p.Close()
	}()

	return nil
}

// readLoop reads raw stdout from master PTY and preserves scrollback.
func (p *Pane) readLoop() {
	buf := make([]byte, 4096)
	lineBuf := strings.Builder{}

	for {
		n, err := p.PTY.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])

			// Broadcast raw bytes to daemon compositor
			select {
			case p.OutputCh <- chunk:
			default:
			}

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
				// PTY closed
			}
			break
		}
	}
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
	p.Bounds.W = cols
	p.Bounds.H = rows
	return p.setWindowSize(cols, rows)
}

func (p *Pane) setWindowSize(cols, rows int) error {
	if p.PTY == nil {
		return nil
	}
	ws := struct {
		Rows    uint16
		Cols    uint16
		Xpixels uint16
		Ypixels uint16
	}{
		Rows: uint16(rows),
		Cols: uint16(cols),
	}
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		p.PTY.Fd(),
		syscall.TIOCSWINSZ,
		uintptr(unsafePointer(&ws)),
	)
	if errno != 0 {
		return errno
	}
	return nil
}

// Close terminates the pane process and releases PTY fds.
func (p *Pane) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.isClosed {
		return nil
	}
	p.isClosed = true

	if p.Cmd != nil && p.Cmd.Process != nil {
		_ = p.Cmd.Process.Signal(syscall.SIGHUP)
	}
	if p.PTY != nil {
		_ = p.PTY.Close()
	}
	close(p.OutputCh)
	return nil
}

// Helper to open PTY without Cgo dependency
func openPTY() (*os.File, *os.File, error) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, err
	}

	sname, err := ptsname(master)
	if err != nil {
		master.Close()
		return nil, nil, err
	}

	if err := grantpt(master); err != nil {
		master.Close()
		return nil, nil, err
	}
	if err := unlockpt(master); err != nil {
		master.Close()
		return nil, nil, err
	}

	slave, err := os.OpenFile(sname, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		return nil, nil, err
	}

	return master, slave, nil
}
