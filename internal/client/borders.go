package client

import (
	"fmt"
	"strings"

	"github.com/SujanPoudel1080/dajimux/internal/protocol"
)

// RenderBorders generates ANSI strings to draw pane frames, titles, and agent status badges with default colors.
func RenderBorders(panes []protocol.PaneInfo, cols, rows int, isMonocle bool) string {
	return RenderBordersWithTheme(panes, cols, rows, isMonocle, "", "")
}

// RenderBordersWithTheme generates ANSI strings with custom active/inactive border colors.
func RenderBordersWithTheme(panes []protocol.PaneInfo, cols, rows int, isMonocle bool, activeColor, inactiveColor string) string {
	if len(panes) <= 1 || isMonocle {
		return ""
	}

	if activeColor == "" {
		activeColor = "\033[1m\033[38;5;51m" // Bold Bright Cyan
	}
	if inactiveColor == "" {
		inactiveColor = "\033[38;5;240m"     // Dim Gray
	}

	var b strings.Builder

	for _, p := range panes {
		if p.W < 4 || p.H < 3 {
			continue
		}

		color := inactiveColor
		if p.IsActive {
			color = activeColor
		}
		reset := "\033[0m"

		// 1. Top border with Title & Agent Badge
		titleStr := fmt.Sprintf(" %s ", p.Title)
		if p.AgentBadge != "" {
			titleStr = fmt.Sprintf(" %s | %s ", p.Title, p.AgentBadge)
		}
		if len(titleStr) > p.W-4 {
			titleStr = titleStr[:p.W-4]
		}

		topDashes := p.W - 2 - len(titleStr)
		if topDashes < 0 {
			topDashes = 0
		}

		b.WriteString(fmt.Sprintf("\033[%d;%dH%s┌%s%s%s┐%s",
			p.Y+1, p.X+1, color, titleStr, color, strings.Repeat("─", topDashes), reset))

		// 2. Vertical borders
		for row := p.Y + 2; row <= p.Y+p.H-1; row++ {
			b.WriteString(fmt.Sprintf("\033[%d;%dH%s│%s", row, p.X+1, color, reset))
			b.WriteString(fmt.Sprintf("\033[%d;%dH%s│%s", row, p.X+p.W, color, reset))
		}

		// 3. Bottom border
		bottomDashes := p.W - 2
		if bottomDashes < 0 {
			bottomDashes = 0
		}
		b.WriteString(fmt.Sprintf("\033[%d;%dH%s└%s┘%s",
			p.Y+p.H, p.X+1, color, strings.Repeat("─", bottomDashes), reset))
	}

	return b.String()
}
