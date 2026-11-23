package client

import (
	"fmt"
	"strings"
)

var defaultHelpLines = []string{
	"MODES & BASICS",
	"  INSERT Mode     Direct shell typing (default mode)",
	"  Space + Space   Enter NORMAL mode (or ^B in prefix mode)",
	"  i, a, Enter     Return to INSERT mode",
	"",
	"PANE NAVIGATION & SPLITS (in Normal Mode)",
	"  h, j, k, l      Spatial pane navigation (left, down, up, right)",
	"  s / v / e       Split active pane: horizontal / vertical / auto",
	"  w               Swap focused pane with next pane in tree",
	"  H, J, K, L      Swap focused pane with spatial neighbor",
	"  t               Cycle layout presets (BSP -> Master-Stack -> Grid)",
	"  z               Toggle zoom / monocle (fullscreen active pane)",
	"  r               Enter resize mode (h/j/k/l or arrow keys to resize)",
	"  x               Close active pane process",
	"",
	"WORKSPACES & SESSIONS",
	"  1 .. 9          Switch to workspace 1..9",
	"  m <1-9>         Move active pane to workspace 1..9",
	"  d               Detach session (leaves background daemon running)",
	"  q               Quit session (terminates current session)",
	"  Q               Quit dajimux completely (stops daemon)",
	"",
	"MOUSE & UTILITIES",
	"  Click Pane      Focus pane | Drag border to resize split ratio",
	"  Scroll Up       Browse scrollback history (vi motions: j/k, y: yank)",
	"  : or Space      Open fuzzy searchable command palette",
	"  ?               Toggle this help screen",
}

// HelpModal manages the quick reference cheat sheet overlay.
type HelpModal struct {
	visible bool
	lines   []string
}

// NewHelpModal initializes a new quick reference modal.
func NewHelpModal() *HelpModal {
	return &HelpModal{
		lines: defaultHelpLines,
	}
}

// IsVisible returns whether the help modal is currently open.
func (h *HelpModal) IsVisible() bool {
	return h.visible
}

// Show marks the modal as active.
func (h *HelpModal) Show() {
	h.visible = true
}

// Hide marks the modal as closed.
func (h *HelpModal) Hide() {
	h.visible = false
}

// Toggle flips visibility and returns the new state.
func (h *HelpModal) Toggle() bool {
	h.visible = !h.visible
	return h.visible
}

// Render generates the ANSI escape sequences for rendering the cheat sheet.
func (h *HelpModal) Render(cols, rows int) string {
	boxWidth := 74
	if boxWidth > cols-4 {
		boxWidth = cols - 4
	}
	if boxWidth < 30 {
		boxWidth = 30
	}

	contentLines := len(h.lines)
	boxHeight := contentLines + 4
	if boxHeight > rows-2 {
		boxHeight = rows - 2
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

	borderFg := "\033[1m\033[38;5;51m" // Bright Cyan
	bg := "\033[48;5;236m"
	fg := "\033[38;5;255m"
	dimFg := "\033[38;5;244m"
	headerFg := "\033[1m\033[38;5;220m" // Gold / Yellow
	reset := "\033[0m"

	// Top border
	title := " dajimux Quick Reference "
	dashCount := boxWidth - 2 - len(title)
	if dashCount < 0 {
		dashCount = 0
	}
	b.WriteString(fmt.Sprintf("\033[%d;%dH%s%s┌%s%s┐%s", startY, startX, bg, borderFg, title, strings.Repeat("─", dashCount), reset))

	// Content lines
	visibleContent := boxHeight - 3
	for i := 0; i < visibleContent; i++ {
		row := startY + 1 + i
		line := ""
		if i < len(h.lines) {
			line = h.lines[i]
		}
		currentFg := fg
		if strings.HasPrefix(line, "MODES") || strings.HasPrefix(line, "PANE") || strings.HasPrefix(line, "WORKSPACES") || strings.HasPrefix(line, "MOUSE") {
			currentFg = headerFg
		}

		lineWidth := len(line)
		if lineWidth > boxWidth-4 {
			line = line[:boxWidth-4]
			lineWidth = boxWidth - 4
		}
		padding := boxWidth - 2 - lineWidth - 2
		if padding < 0 {
			padding = 0
		}
		b.WriteString(fmt.Sprintf("\033[%d;%dH%s%s│%s  %s%s%s%s│%s",
			row, startX, bg, borderFg, reset+bg, currentFg, line, strings.Repeat(" ", padding), borderFg, reset))
	}

	// Bottom border with dismiss prompt
	dismiss := " Press Esc, q, or ? to dismiss "
	bottomDashes := boxWidth - 2 - len(dismiss)
	if bottomDashes < 0 {
		bottomDashes = 0
	}
	b.WriteString(fmt.Sprintf("\033[%d;%dH%s%s└%s%s%s%s┘%s",
		startY+boxHeight-1, startX, bg, borderFg, dimFg, dismiss, borderFg, strings.Repeat("─", bottomDashes), reset))

	b.WriteString("\0338") // Restore cursor
	return b.String()
}
