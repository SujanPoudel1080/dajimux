package client

import (
	"fmt"
	"strings"

	"github.com/SujanPoudel1080/dajimux/internal/config"
	"github.com/SujanPoudel1080/dajimux/internal/protocol"
	"github.com/SujanPoudel1080/dajimux/internal/sysstats"
)

// ChromeRenderer handles drawing the top tab bar, pane borders, and bottom status bar.
type ChromeRenderer struct {
	config           *config.Config
	statsCollector   *sysstats.Collector
	lastRenderedCols int
	lastRenderedRows int
}

// NewChromeRenderer creates a new chrome renderer.
func NewChromeRenderer(cfg *config.Config, statsCol *sysstats.Collector) *ChromeRenderer {
	return &ChromeRenderer{
		config:         cfg,
		statsCollector: statsCol,
	}
}

// SetConfig updates user configuration.
func (r *ChromeRenderer) SetConfig(cfg *config.Config) {
	r.config = cfg
}

// SetStatsCollector sets the system telemetry collector.
func (r *ChromeRenderer) SetStatsCollector(statsCol *sysstats.Collector) {
	r.statsCollector = statsCol
}

// Render generates the ANSI sequence for drawing tab bar, borders, and footer.
// It returns the escape string and whether terminal dimensions changed since last render.
func (r *ChromeRenderer) Render(
	state protocol.LayoutStatePayload,
	cols, rows int,
	inPrefix bool,
	leaderHint string,
	compositors map[string]*ANSICompositor,
) (string, bool) {
	sizeChanged := cols != r.lastRenderedCols || rows != r.lastRenderedRows
	r.lastRenderedCols = cols
	r.lastRenderedRows = rows

	var b strings.Builder
	b.WriteString("\0337") // Save cursor

	if sizeChanged {
		// Clear screen to erase stale borders and previous overlays
		b.WriteString("\033[H\033[2J\033[r")
	}

	// 1. Top Header (Workspaces & Mode)
	b.WriteString("\033[1;1H")

	headerBg := "\033[48;5;236m"
	headerFg := "\033[38;5;255m"
	activeBorder := ""
	inactiveBorder := ""
	footerBg := "\033[48;5;236m"
	footerFg := "\033[38;5;250m"

	if r.config != nil {
		if r.config.Theme.StatusBarBg != "" {
			headerBg = r.config.Theme.StatusBarBg
			footerBg = r.config.Theme.StatusBarBg
		}
		if r.config.Theme.StatusBarFg != "" {
			footerFg = r.config.Theme.StatusBarFg
		}
		activeBorder = r.config.Theme.BorderActive
		inactiveBorder = r.config.Theme.BorderInactive
	}

	b.WriteString(headerBg)
	b.WriteString(headerFg)

	// Mode badge color
	modeText := string(state.Mode)
	modeColor := "\033[48;5;28m\033[38;5;255m" // Green for Insert
	if inPrefix {
		modeText = "PREFIX"
		modeColor = "\033[48;5;33m\033[38;5;255m\033[1m" // Bright Blue for Prefix awaiting command
	} else if state.Mode == protocol.ModeNormal {
		modeColor = "\033[48;5;166m\033[38;5;255m" // Orange for Normal
	} else if state.Mode == protocol.ModeResize {
		modeColor = "\033[48;5;160m\033[38;5;255m" // Red for Resize
	}

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

	// System stats badge (CPU & RAM)
	var statsPlain, statsANSI string
	if r.statsCollector != nil {
		st := r.statsCollector.Stats()
		avail := cols - printedHeaderLen - len(layoutBadge)
		statsPlain, statsANSI = formatHeaderStats(st, avail, headerBg, headerFg)
	}

	rightLen := len(layoutBadge)
	if statsPlain != "" {
		rightLen += len(statsPlain) + 1
	}

	// Pad header and place stats & layout indicator at far right
	if cols > printedHeaderLen+rightLen {
		pad := cols - printedHeaderLen - rightLen
		b.WriteString(strings.Repeat(" ", pad))
		if statsANSI != "" {
			b.WriteString(statsANSI + " ")
		}
		b.WriteString("\033[1m\033[38;5;51m" + layoutBadge + "\033[0m" + headerBg + headerFg)
	} else if cols > printedHeaderLen+len(layoutBadge) {
		pad := cols - printedHeaderLen - len(layoutBadge)
		b.WriteString(strings.Repeat(" ", pad))
		b.WriteString("\033[1m\033[38;5;51m" + layoutBadge + "\033[0m" + headerBg + headerFg)
	} else if cols > printedHeaderLen {
		b.WriteString(strings.Repeat(" ", cols-printedHeaderLen))
	}
	b.WriteString("\033[0m")

	// 2. Borders and Pane Headers
	b.WriteString(RenderBordersWithTheme(state.Panes, cols, rows, state.IsMonocle, activeBorder, inactiveBorder))

	// 3. Bottom Status Bar (Agent Telemetry & Shortcuts)
	b.WriteString(fmt.Sprintf("\033[%d;1H", rows))
	b.WriteString(footerBg)
	b.WriteString(footerFg)

	var agentTelemetry string
	for _, p := range state.Panes {
		if p.IsActive && p.AgentBadge != "" {
			agentTelemetry = fmt.Sprintf(" | Agent: %s", p.AgentBadge)
			break
		}
	}

	var footerText string
	if inPrefix {
		footerText = fmt.Sprintf(" [PREFIX] s/v/e: Split | h/j/k/l: Nav | w: Swap | t: Layout | z: Zoom | d: Detach | x: Close | Esc: Cancel%s", agentTelemetry)
	} else if state.Mode == protocol.ModeNormal {
		footerText = fmt.Sprintf(" [NORMAL] i/Enter: Insert | h/j/k/l: Nav | s/v/e: Split | w: Swap | t: Layout | z: Zoom | d: Detach | x: Close | ?: Help%s", agentTelemetry)
	} else if state.Mode == protocol.ModeResize {
		footerText = fmt.Sprintf(" [RESIZE] h/j/k/l: Adjust Size | Enter/Esc: Normal Mode%s", agentTelemetry)
	} else {
		// INSERT Mode
		if leaderHint == "" {
			leaderHint = "Space+Space: Normal Mode"
		}
		footerText = fmt.Sprintf(" [INSERT] %s | Click to Focus | Drag Borders to Resize%s", leaderHint, agentTelemetry)
	}

	b.WriteString(footerText)
	if cols > len(footerText) {
		b.WriteString(strings.Repeat(" ", cols-len(footerText)))
	}
	b.WriteString("\033[0m")

	// Position hardware cursor inside active pane viewport
	activeY := 2
	activeX := 1
	hasBorders := len(state.Panes) > 1 && !state.IsMonocle
	for _, p := range state.Panes {
		if p.IsActive {
			if comp, ok := compositors[p.ID]; ok && comp != nil {
				activeY = comp.curRow + comp.offY - 1
				activeX = comp.curCol + comp.offX - 1
			} else if hasBorders {
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

	return b.String(), sizeChanged
}

// RenderTopStats writes an in-place telemetry update on Row 1 without disturbing cursor position or pane buffers.
func (r *ChromeRenderer) RenderTopStats(state protocol.LayoutStatePayload, cols int, inPrefix bool) string {
	if r.statsCollector == nil || cols < 40 {
		return ""
	}

	headerBg := "\033[48;5;236m"
	headerFg := "\033[38;5;255m"
	if r.config != nil && r.config.Theme.StatusBarBg != "" {
		headerBg = r.config.Theme.StatusBarBg
	}
	if r.config != nil && r.config.Theme.StatusBarFg != "" {
		headerFg = r.config.Theme.StatusBarFg
	}

	modeText := string(state.Mode)
	if inPrefix {
		modeText = "PREFIX"
	}
	printedHeaderLen := len(modeText) + 2 + len(state.SessionName) + 12

	wsNames := make(map[int]string)
	for _, ws := range state.Workspaces {
		wsNames[ws.ID] = ws.Name
	}
	for i := 1; i <= 9; i++ {
		name := wsNames[i]
		hasCustomName := name != "" && name != fmt.Sprintf("ws-%d", i) && (i != 1 || name != "main")
		tabLabel := fmt.Sprintf("%d", i)
		if hasCustomName {
			tabLabel = fmt.Sprintf("%d:%s", i, name)
		}
		if i == state.ActiveWorkspace {
			printedHeaderLen += len(tabLabel) + 3
		} else if i <= state.TotalWorkspaces {
			printedHeaderLen += len(tabLabel) + 2
		}
	}

	layoutStr := strings.ToUpper(state.LayoutType)
	if layoutStr == "" || layoutStr == "BSP" {
		layoutStr = "BSP"
	} else if layoutStr == "MASTER_STACK" {
		layoutStr = "MASTER"
	} else if layoutStr == "GRID" {
		layoutStr = "GRID"
	}
	layoutBadge := fmt.Sprintf(" <%s> ", layoutStr)

	st := r.statsCollector.Stats()
	avail := cols - printedHeaderLen - len(layoutBadge)
	statsPlain, statsANSI := formatHeaderStats(st, avail, headerBg, headerFg)
	if statsPlain == "" {
		return ""
	}

	rightLen := len(statsPlain) + 1 + len(layoutBadge)
	startCol := cols - rightLen + 1
	if startCol <= printedHeaderLen {
		return ""
	}

	var b strings.Builder
	b.WriteString("\0337") // Save hardware cursor
	b.WriteString(fmt.Sprintf("\033[1;%dH", startCol))
	b.WriteString(headerBg + headerFg)
	b.WriteString(statsANSI + " ")
	b.WriteString("\033[1m\033[38;5;51m" + layoutBadge + "\033[0m")
	b.WriteString("\0338") // Restore hardware cursor
	return b.String()
}

// WorkspaceFromHeaderClick calculates which workspace tab was clicked in row 1.
func (r *ChromeRenderer) WorkspaceFromHeaderClick(clickX int, state protocol.LayoutStatePayload, inPrefix bool) int {
	mode := string(state.Mode)
	if inPrefix {
		mode = "PREFIX"
	} else if mode == "" {
		mode = string(protocol.ModeInsert)
	}

	sessionName := state.SessionName
	if sessionName == "" {
		sessionName = "main"
	}

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

		width := len(tabLabel) + 2
		if i == state.ActiveWorkspace {
			width = len(tabLabel) + 3
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

// formatHeaderStats selects the best fitting system stats representation for available header space.
func formatHeaderStats(st sysstats.Stats, availSpace int, headerBg, headerFg string) (string, string) {
	full := st.Format()
	compact := st.FormatCompact()
	short := fmt.Sprintf("CPU %.0f%%", st.CPUPercent)

	if availSpace >= len(full)+2 {
		return full, st.FormatANSI(headerBg, headerFg)
	}
	if availSpace >= len(compact)+2 {
		return compact, st.FormatANSI(headerBg, headerFg)
	}
	if availSpace >= len(short)+2 {
		return short, short
	}
	return "", ""
}
