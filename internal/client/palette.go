package client

import (
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
}

// DefaultPaletteItems returns the built-in commands available in the palette.
func DefaultPaletteItems() []PaletteItem {
	return []PaletteItem{
		{ID: "split_v", Title: "Split Vertical", Description: "Split active pane vertically (left/right)", Action: ActionSplitVertical},
		{ID: "split_h", Title: "Split Horizontal", Description: "Split active pane horizontally (top/bottom)", Action: ActionSplitHorizontal},
		{ID: "close", Title: "Close Pane", Description: "Terminate process and close pane", Action: ActionClosePane},
		{ID: "zoom", Title: "Toggle Monocle (Zoom)", Description: "Maximize or restore current pane", Action: ActionToggleMonocle},
		{ID: "resize", Title: "Resize Mode", Description: "Fine-tune pane split ratios", Action: ActionModeChange},
		{ID: "scrollback", Title: "Scrollback Mode", Description: "Browse scrollback history and copy text", Action: ActionEnterScrollback},
		{ID: "detach", Title: "Detach Session", Description: "Disconnect client and keep session running in daemon", Action: ActionDetach},
		{ID: "ws1", Title: "Go to Workspace 1", Description: "Switch to workspace 1", Action: ActionSwitchWorkspace, Workspace: 1},
		{ID: "ws2", Title: "Go to Workspace 2", Description: "Switch to workspace 2", Action: ActionSwitchWorkspace, Workspace: 2},
		{ID: "ws3", Title: "Go to Workspace 3", Description: "Switch to workspace 3", Action: ActionSwitchWorkspace, Workspace: 3},
		{ID: "ws4", Title: "Go to Workspace 4", Description: "Switch to workspace 4", Action: ActionSwitchWorkspace, Workspace: 4},
	}
}

// NewPalette creates a command palette initialized with default commands.
func NewPalette() *Palette {
	return &Palette{
		Items: DefaultPaletteItems(),
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

// MoveDown increments selection.
func (p *Palette) MoveDown(max int) {
	if p.SelectedIndex < max-1 {
		p.SelectedIndex++
	}
}

// MoveUp decrements selection.
func (p *Palette) MoveUp() {
	if p.SelectedIndex > 0 {
		p.SelectedIndex--
	}
}

// TypeChar appends a rune to the query and resets selection.
func (p *Palette) TypeChar(r rune) {
	p.Query += string(r)
	p.SelectedIndex = 0
}

// Backspace removes the last character from the query.
func (p *Palette) Backspace() {
	if len(p.Query) > 0 {
		runes := []rune(p.Query)
		p.Query = string(runes[:len(runes)-1])
		p.SelectedIndex = 0
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
