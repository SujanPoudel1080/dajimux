package client

import (
	"fmt"
	"strings"
)

// PaletteOverlay manages layout positioning, rendering, and clearing of the floating palette overlay.
type PaletteOverlay struct {
	prevX int
	prevY int
	prevW int
	prevH int
}

// NewPaletteOverlay creates a new palette overlay renderer.
func NewPaletteOverlay() *PaletteOverlay {
	return &PaletteOverlay{}
}

// Clear returns the ANSI sequence to erase the previously drawn palette bounding box.
func (p *PaletteOverlay) Clear() (string, bool) {
	if p.prevH <= 0 || p.prevW <= 0 {
		return "", false
	}

	var b strings.Builder
	b.WriteString("\0337") // Save cursor
	for y := p.prevY; y <= p.prevY+p.prevH; y++ {
		b.WriteString(fmt.Sprintf("\033[%d;%dH%s", y, p.prevX, strings.Repeat(" ", p.prevW)))
	}
	b.WriteString("\0338") // Restore cursor

	p.prevX, p.prevY, p.prevW, p.prevH = 0, 0, 0, 0
	return b.String(), true
}

// Reset clears the cached geometry without generating escape sequences.
func (p *PaletteOverlay) Reset() {
	p.prevX, p.prevY, p.prevW, p.prevH = 0, 0, 0, 0
}

// Render returns the ANSI escape sequence to draw the command palette centered on screen.
func (p *PaletteOverlay) Render(pal *Palette, cols, rows int) string {
	if pal == nil {
		return ""
	}

	items := pal.Filter()
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
	if p.prevH > 0 && (p.prevX != startX || p.prevY != startY || p.prevW != boxWidth || p.prevH != boxHeight) {
		for y := p.prevY; y < p.prevY+p.prevH; y++ {
			b.WriteString(fmt.Sprintf("\033[%d;%dH%s", y, p.prevX, strings.Repeat(" ", p.prevW)))
		}
	}
	p.prevX = startX
	p.prevY = startY
	p.prevW = boxWidth
	p.prevH = boxHeight

	// Palette Border & Colors
	bg := "\033[48;5;235m"
	fg := "\033[38;5;255m"
	borderFg := "\033[38;5;51m"
	reset := "\033[0m"

	// Adjust scroll window so SelectedIndex is always visible
	pal.AdjustScroll(maxVisibleItems, len(items))

	// 1. Top border with title and item count
	title := " Command Palette "
	countBadge := ""
	if len(items) > 0 {
		countBadge = fmt.Sprintf(" [%d/%d] ", pal.SelectedIndex+1, len(items))
	}
	dashes := boxWidth - 2 - len(title) - len(countBadge)
	if dashes < 0 {
		dashes = 0
	}
	b.WriteString(fmt.Sprintf("\033[%d;%dH%s%s┌─%s%s%s%s%s%s┐%s",
		startY, startX, bg, borderFg, fg, title, borderFg, strings.Repeat("─", dashes), fg, countBadge, reset))

	// 2. Query input line
	queryPrompt := fmt.Sprintf("> %s_", pal.Query)
	padQuery := boxWidth - 4 - len(queryPrompt)
	if padQuery < 0 {
		padQuery = 0
	}
	b.WriteString(fmt.Sprintf("\033[%d;%dH%s%s│ %s\033[1m%s\033[0m%s%s%s│%s",
		startY+1, startX, bg, borderFg, fg, queryPrompt, bg, strings.Repeat(" ", padQuery), borderFg, reset))

	// 3. Separator with scroll-up indicator
	scrollUpInd := "─"
	if pal.ScrollOffset > 0 {
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
		itemIdx := pal.ScrollOffset + i
		if itemIdx < len(items) {
			item := items[itemIdx]
			prefix := "  "
			lineBg := bg
			lineFg := fg
			if itemIdx == pal.SelectedIndex {
				prefix = "> "
				lineBg = "\033[48;5;24m"
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
			padEmpty := boxWidth - 2
			b.WriteString(fmt.Sprintf("\033[%d;%dH%s%s│%s%s│%s",
				slotY, startX, borderFg, bg, strings.Repeat(" ", padEmpty), borderFg, reset))
		}
	}

	// 5. Bottom border with scroll-down indicator & keybinding hint
	bottomY := startY + 3 + maxVisibleItems
	scrollDownInd := ""
	if pal.ScrollOffset+maxVisibleItems < len(items) {
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
	return b.String()
}
