package client

import (
	"fmt"
	"strings"
)

// ScrollbackViewer manages the scrollback buffer viewport, navigation, and rendering.
type ScrollbackViewer struct {
	lines  []string
	offset int
	toast  string
}

// NewScrollbackViewer creates a new scrollback viewer.
func NewScrollbackViewer() *ScrollbackViewer {
	return &ScrollbackViewer{}
}

// SetLines updates the buffer lines and resets the offset to the bottom.
func (s *ScrollbackViewer) SetLines(lines []string) {
	s.lines = lines
	s.offset = 0
	s.toast = ""
}

// Active returns true if there is an active scrollback buffer loaded.
func (s *ScrollbackViewer) Active() bool {
	return len(s.lines) > 0
}

// Clear resets the scrollback state.
func (s *ScrollbackViewer) Clear() {
	s.lines = nil
	s.offset = 0
	s.toast = ""
}

// Lines returns the full scrollback buffer.
func (s *ScrollbackViewer) Lines() []string {
	return s.lines
}

// Offset returns the current scroll offset from the bottom.
func (s *ScrollbackViewer) Offset() int {
	return s.offset
}

// SetOffset sets the scroll offset directly.
func (s *ScrollbackViewer) SetOffset(offset int) {
	s.offset = offset
}

// MaxOffset returns the maximum allowed scroll offset given terminal rows.
func (s *ScrollbackViewer) MaxOffset(rows int) int {
	max := len(s.lines) - (rows - 2)
	if max < 0 {
		return 0
	}
	return max
}

// ScrollUp moves the viewport up by delta lines.
func (s *ScrollbackViewer) ScrollUp(delta, rows int) {
	max := s.MaxOffset(rows)
	s.offset += delta
	if s.offset > max {
		s.offset = max
	}
}

// ScrollDown moves the viewport down by delta lines.
func (s *ScrollbackViewer) ScrollDown(delta int) {
	s.offset -= delta
	if s.offset < 0 {
		s.offset = 0
	}
}

// ScrollTop jumps to the oldest lines in the buffer.
func (s *ScrollbackViewer) ScrollTop(rows int) {
	s.offset = s.MaxOffset(rows)
}

// ScrollBottom jumps to the newest lines in the buffer.
func (s *ScrollbackViewer) ScrollBottom() {
	s.offset = 0
}

// SetToast sets a temporary notification message on the scrollback footer.
func (s *ScrollbackViewer) SetToast(msg string) {
	s.toast = msg
}

// ClearToast removes any active toast notification.
func (s *ScrollbackViewer) ClearToast() {
	s.toast = ""
}

// VisibleText returns the text currently visible in the viewport and its line count.
func (s *ScrollbackViewer) VisibleText(rows int) (string, int) {
	if len(s.lines) == 0 {
		return "", 0
	}
	viewHeight := rows - 2
	if viewHeight < 1 {
		viewHeight = 1
	}

	total := len(s.lines)
	endIdx := total - s.offset
	startIdx := endIdx - viewHeight
	if startIdx < 0 {
		startIdx = 0
	}
	if endIdx > total {
		endIdx = total
	}
	if startIdx >= endIdx {
		return "", 0
	}
	return strings.Join(s.lines[startIdx:endIdx], "\n"), endIdx - startIdx
}

// Render returns the ANSI escape sequence to draw the scrollback viewport.
func (s *ScrollbackViewer) Render(cols, rows int) string {
	var b strings.Builder
	b.WriteString("\0337") // Save cursor

	viewHeight := rows - 2
	if viewHeight < 1 {
		viewHeight = 1
	}

	total := len(s.lines)
	endIdx := total - s.offset
	startIdx := endIdx - viewHeight
	if startIdx < 0 {
		startIdx = 0
	}
	if endIdx < 0 {
		endIdx = 0
	}
	if endIdx > total {
		endIdx = total
	}

	// Render lines in the viewport
	row := 2
	for i := startIdx; i < endIdx && row < rows; i++ {
		line := s.lines[i]
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
		total-s.offset, total)
	if s.toast != "" {
		bannerBg = "\033[48;5;28m\033[38;5;255m\033[1m" // Bright green banner for toast confirmation
		status = fmt.Sprintf(" %s ", s.toast)
	}

	b.WriteString(fmt.Sprintf("\033[%d;1H%s", rows, bannerBg))
	b.WriteString(status)
	if cols > len(status) {
		b.WriteString(strings.Repeat(" ", cols-len(status)))
	}
	b.WriteString("\033[0m")

	b.WriteString("\0338") // Restore cursor
	return b.String()
}
