package client

import (
	"fmt"
	"strings"
	"unicode"
)

// PaletteItem represents an actionable entry in the command palette.
type PaletteItem struct {
	ID          string
	Title       string
	Description string
	Action      ActionType
	Workspace   int
}

// Palette manages interactive fuzzy matching for the command palette.
type Palette struct {
	Query         string
	Items         []PaletteItem
	SelectedIndex int
	ScrollOffset  int
}

// PaletteItemsForWorkspaces returns the built-in commands with workspace entries up to max(9, totalWS).
func PaletteItemsForWorkspaces(totalWS int) []PaletteItem {
	items := []PaletteItem{
		{ID: "split_v", Title: "Split Vertical", Description: "Split active pane vertically (left/right)", Action: ActionSplitVertical},
		{ID: "split_h", Title: "Split Horizontal", Description: "Split active pane horizontally (top/bottom)", Action: ActionSplitHorizontal},
		{ID: "close", Title: "Close Pane", Description: "Terminate process and close pane", Action: ActionClosePane},
		{ID: "zoom", Title: "Toggle Monocle (Zoom)", Description: "Maximize or restore current pane", Action: ActionToggleMonocle},
		{ID: "resize", Title: "Resize Mode", Description: "Fine-tune pane split ratios", Action: ActionModeChange},
		{ID: "scrollback", Title: "Scrollback Mode", Description: "Browse scrollback history and copy text", Action: ActionEnterScrollback},
		{ID: "detach", Title: "Detach Session", Description: "Disconnect client and keep session running in daemon", Action: ActionDetach},
	}

	maxWS := totalWS
	if maxWS < 9 {
		maxWS = 9
	}
	for i := 1; i <= maxWS; i++ {
		items = append(items, PaletteItem{
			ID:          fmt.Sprintf("ws%d", i),
			Title:       fmt.Sprintf("Go to Workspace %d", i),
			Description: fmt.Sprintf("Switch to workspace %d", i),
			Action:      ActionSwitchWorkspace,
			Workspace:   i,
		})
	}
	return items
}

// DefaultPaletteItems returns the built-in commands available in the palette.
func DefaultPaletteItems() []PaletteItem {
	return PaletteItemsForWorkspaces(9)
}

// NewPalette creates a command palette initialized with default commands.
func NewPalette() *Palette {
	return &Palette{
		Items: DefaultPaletteItems(),
	}
}

// NewPaletteWithWorkspaces creates a command palette initialized with commands for the given workspace count.
func NewPaletteWithWorkspaces(totalWS int) *Palette {
	return &Palette{
		Items: PaletteItemsForWorkspaces(totalWS),
	}
}

// Filter returns items matching the query using substring and fuzzy matching.
func (p *Palette) Filter() []PaletteItem {
	if strings.TrimSpace(p.Query) == "" {
		return p.Items
	}

	query := strings.ToLower(strings.TrimSpace(p.Query))
	var titleMatches []PaletteItem
	var descMatches []PaletteItem
	var fuzzyMatches []PaletteItem

	for _, item := range p.Items {
		title := strings.ToLower(item.Title)
		desc := strings.ToLower(item.Description)

		if strings.Contains(title, query) {
			titleMatches = append(titleMatches, item)
		} else if strings.Contains(desc, query) {
			descMatches = append(descMatches, item)
		} else if fuzzyMatch(query, title) {
			fuzzyMatches = append(fuzzyMatches, item)
		}
	}

	res := append(titleMatches, descMatches...)
	return append(res, fuzzyMatches...)
}

// MoveDown increments selection with wrap-around.
func (p *Palette) MoveDown(max int) {
	if max <= 0 {
		return
	}
	if p.SelectedIndex < max-1 {
		p.SelectedIndex++
	} else {
		p.SelectedIndex = 0
	}
}

// MoveUp decrements selection with wrap-around.
func (p *Palette) MoveUp(max int) {
	if max <= 0 {
		return
	}
	if p.SelectedIndex > 0 {
		p.SelectedIndex--
	} else {
		p.SelectedIndex = max - 1
	}
}

// AdjustScroll keeps SelectedIndex within the visible window [ScrollOffset, ScrollOffset+maxVisible).
func (p *Palette) AdjustScroll(maxVisible, totalItems int) {
	if totalItems <= maxVisible {
		p.ScrollOffset = 0
		return
	}

	if p.SelectedIndex < p.ScrollOffset {
		p.ScrollOffset = p.SelectedIndex
	}
	if p.SelectedIndex >= p.ScrollOffset+maxVisible {
		p.ScrollOffset = p.SelectedIndex - maxVisible + 1
	}

	maxScroll := totalItems - maxVisible
	if p.ScrollOffset > maxScroll {
		p.ScrollOffset = maxScroll
	}
	if p.ScrollOffset < 0 {
		p.ScrollOffset = 0
	}
}

// TypeChar appends a rune to the query and resets selection and scroll.
func (p *Palette) TypeChar(r rune) {
	p.Query += string(r)
	p.SelectedIndex = 0
	p.ScrollOffset = 0
}

// Backspace removes the last character from the query and resets selection and scroll.
func (p *Palette) Backspace() {
	if len(p.Query) > 0 {
		runes := []rune(p.Query)
		p.Query = string(runes[:len(runes)-1])
		p.SelectedIndex = 0
		p.ScrollOffset = 0
	}
}

func fuzzyMatch(pattern, text string) bool {
	pRunes := []rune(pattern)
	tRunes := []rune(text)

	pIdx := 0
	tIdx := 0

	for pIdx < len(pRunes) && tIdx < len(tRunes) {
		if unicode.ToLower(pRunes[pIdx]) == unicode.ToLower(tRunes[tIdx]) {
			pIdx++
		}
		tIdx++
	}

	return pIdx == len(pRunes)
}
