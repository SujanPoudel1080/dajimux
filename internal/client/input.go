package client

import (
	"time"

	"github.com/SujanPoudel1080/dajimux/internal/protocol"
)

// InputHandler processes raw input bytes and manages modal state transitions.
type InputHandler struct {
	CurrentMode        protocol.Mode
	doubleTapThreshold time.Duration
	lastSpaceTime      time.Time
}

// ActionType represents an action triggered in NORMAL or RESIZE mode.
type ActionType string

const (
	ActionNone            ActionType = "none"
	ActionPassThrough     ActionType = "passthrough"
	ActionModeChange      ActionType = "mode_change"
	ActionUndoLastChar    ActionType = "undo_last_char"
	ActionNavLeft         ActionType = "nav_left"
	ActionNavDown         ActionType = "nav_down"
	ActionNavUp           ActionType = "nav_up"
	ActionNavRight        ActionType = "nav_right"
	ActionSplitHorizontal ActionType = "split_h"
	ActionSplitVertical   ActionType = "split_v"
	ActionClosePane       ActionType = "close_pane"
	ActionToggleMonocle   ActionType = "toggle_monocle"
	ActionSwitchWorkspace ActionType = "switch_workspace"
	ActionResizeDelta     ActionType = "resize_delta"
	ActionOpenPalette      ActionType = "open_palette"
	ActionClosePalette     ActionType = "close_palette"
	ActionPaletteType      ActionType = "palette_type"
	ActionPaletteBackspace ActionType = "palette_backspace"
	ActionPaletteUp        ActionType = "palette_up"
	ActionPaletteDown      ActionType = "palette_down"
	ActionExecutePalette   ActionType = "palette_execute"
	ActionEnterScrollback  ActionType = "enter_scrollback"
	ActionExitScrollback   ActionType = "exit_scrollback"
	ActionScrollUp         ActionType = "scroll_up"
	ActionScrollDown       ActionType = "scroll_down"
	ActionScrollPageUp     ActionType = "scroll_page_up"
	ActionScrollPageDown   ActionType = "scroll_page_down"
	ActionScrollTop        ActionType = "scroll_top"
	ActionScrollBottom     ActionType = "scroll_bottom"
	ActionYank             ActionType = "yank"
)

// InputResult contains the outcome of processing user keystrokes.
type InputResult struct {
	Action      ActionType
	TargetMode  protocol.Mode
	WorkspaceNum int
	ResizeDelta float64
	RawForward  []byte // Raw bytes to stream to PTY master
	UndoBytes   []byte // Optional undo sequence (e.g. backspace) to remove prefix
}

// NewInputHandler creates an input handler with 280ms double-tap space threshold.
func NewInputHandler() *InputHandler {
	return &InputHandler{
		CurrentMode:        protocol.ModeInsert,
		doubleTapThreshold: 280 * time.Millisecond,
	}
}

// HandleKey processes a sequence of input bytes based on the active mode.
func (h *InputHandler) HandleKey(data []byte) InputResult {
	if len(data) == 0 {
		return InputResult{Action: ActionNone}
	}

	switch h.CurrentMode {
	case protocol.ModeInsert:
		return h.handleInsertMode(data)
	case protocol.ModeNormal:
		return h.handleNormalMode(data)
	case protocol.ModeResize:
		return h.handleResizeMode(data)
	case protocol.ModeScrollback:
		return h.handleScrollbackMode(data)
	case protocol.ModeCommandPalette:
		return h.handlePaletteMode(data)
	default:
		h.CurrentMode = protocol.ModeInsert
		return InputResult{Action: ActionPassThrough, RawForward: data}
	}
}

func (h *InputHandler) handleInsertMode(data []byte) InputResult {
	// Check for double-tapping of Space key (0x20)
	if len(data) == 1 && data[0] == ' ' {
		now := time.Now()
		if !h.lastSpaceTime.IsZero() && now.Sub(h.lastSpaceTime) < h.doubleTapThreshold {
			// Double-tap Space detected!
			h.lastSpaceTime = time.Time{}
			h.CurrentMode = protocol.ModeNormal

			// Send backspace (\x7f) to undo the first space sent to the shell/editor
			return InputResult{
				Action:     ActionModeChange,
				TargetMode: protocol.ModeNormal,
				UndoBytes:  []byte{0x7f}, // Rubout / Backspace
			}
		}

		h.lastSpaceTime = now
		return InputResult{
			Action:     ActionPassThrough,
			RawForward: data,
		}
	}

	// Any non-space key resets the double-tap timer
	h.lastSpaceTime = time.Time{}
	return InputResult{
		Action:     ActionPassThrough,
		RawForward: data,
	}
}

func (h *InputHandler) handleNormalMode(data []byte) InputResult {
	if len(data) == 1 {
		b := data[0]

		// Switch back to Insert Mode: 'i', 'a', or Enter (0x0d)
		if b == 'i' || b == 'a' || b == '\r' || b == '\n' {
			h.CurrentMode = protocol.ModeInsert
			return InputResult{Action: ActionModeChange, TargetMode: protocol.ModeInsert}
		}

		// Vim Directional Navigation (h, j, k, l)
		switch b {
		case 'h':
			return InputResult{Action: ActionNavLeft}
		case 'j':
			return InputResult{Action: ActionNavDown}
		case 'k':
			return InputResult{Action: ActionNavUp}
		case 'l':
			return InputResult{Action: ActionNavRight}
		case 's': // Split Horizontal
			return InputResult{Action: ActionSplitHorizontal}
		case 'v': // Split Vertical
			return InputResult{Action: ActionSplitVertical}
		case 'x': // Close Pane
			return InputResult{Action: ActionClosePane}
		case 'z': // Toggle Monocle / Zoom
			return InputResult{Action: ActionToggleMonocle}
		case 'r': // Enter Resize Mode
			h.CurrentMode = protocol.ModeResize
			return InputResult{Action: ActionModeChange, TargetMode: protocol.ModeResize}
		case ':', ' ': // Open Command Palette
			h.CurrentMode = protocol.ModeCommandPalette
			return InputResult{Action: ActionOpenPalette, TargetMode: protocol.ModeCommandPalette}
		case '[', 0x15: // Enter Scrollback Vi-Mode
			h.CurrentMode = protocol.ModeScrollback
			return InputResult{Action: ActionEnterScrollback, TargetMode: protocol.ModeScrollback}
		}

		// Workspace Switching: '1' through '9'
		if b >= '1' && b <= '9' {
			ws := int(b - '0')
			return InputResult{
				Action:       ActionSwitchWorkspace,
				WorkspaceNum: ws,
			}
		}
	}

	return InputResult{Action: ActionNone}
}

func (h *InputHandler) handleScrollbackMode(data []byte) InputResult {
	if len(data) == 1 {
		b := data[0]

		// Exit Scrollback mode on q, Esc (0x1b), or Enter
		if b == 'q' || b == 0x1b || b == '\r' || b == '\n' {
			h.CurrentMode = protocol.ModeNormal
			return InputResult{Action: ActionExitScrollback, TargetMode: protocol.ModeNormal}
		}

		switch b {
		case 'k': // Scroll up 1 line
			return InputResult{Action: ActionScrollUp}
		case 'j': // Scroll down 1 line
			return InputResult{Action: ActionScrollDown}
		case 0x15: // Ctrl-u (Page up)
			return InputResult{Action: ActionScrollPageUp}
		case 0x04: // Ctrl-d (Page down)
			return InputResult{Action: ActionScrollPageDown}
		case 'g': // Jump to top
			return InputResult{Action: ActionScrollTop}
		case 'G': // Jump to bottom
			return InputResult{Action: ActionScrollBottom}
		case 'y': // Yank visible text to system clipboard via OSC 52
			return InputResult{Action: ActionYank}
		}
	} else if len(data) == 3 && data[0] == 0x1b && data[1] == '[' {
		switch data[2] {
		case 'A': // Up arrow
			return InputResult{Action: ActionScrollUp}
		case 'B': // Down arrow
			return InputResult{Action: ActionScrollDown}
		}
	}

	return InputResult{Action: ActionNone}
}

func (h *InputHandler) handleResizeMode(data []byte) InputResult {
	if len(data) == 1 {
		b := data[0]

		// Escape (0x1b) or Enter exits Resize mode back to Normal mode
		if b == 0x1b || b == '\r' || b == '\n' || b == 'q' {
			h.CurrentMode = protocol.ModeNormal
			return InputResult{Action: ActionModeChange, TargetMode: protocol.ModeNormal}
		}

		// Adjust border ratio
		switch b {
		case 'h', 'k': // Shrink active pane
			return InputResult{Action: ActionResizeDelta, ResizeDelta: -0.05}
		case 'l', 'j': // Expand active pane
			return InputResult{Action: ActionResizeDelta, ResizeDelta: 0.05}
		}
	}

	return InputResult{Action: ActionNone}
}

func (h *InputHandler) handlePaletteMode(data []byte) InputResult {
	if len(data) == 1 {
		b := data[0]
		if b == 0x1b { // Escape closes palette
			h.CurrentMode = protocol.ModeNormal
			return InputResult{Action: ActionClosePalette, TargetMode: protocol.ModeNormal}
		}
		if b == '\r' || b == '\n' { // Enter executes
			return InputResult{Action: ActionExecutePalette}
		}
		if b == 0x7f || b == 0x08 { // Backspace
			return InputResult{Action: ActionPaletteBackspace}
		}
		if b == 0x0e { // Ctrl-N / Down
			return InputResult{Action: ActionPaletteDown}
		}
		if b == 0x10 { // Ctrl-P / Up
			return InputResult{Action: ActionPaletteUp}
		}
		if b >= 32 && b < 127 { // Printable character
			return InputResult{Action: ActionPaletteType, RawForward: data}
		}
	} else if len(data) == 3 && data[0] == 0x1b && data[1] == '[' {
		switch data[2] {
		case 'A': // Up arrow
			return InputResult{Action: ActionPaletteUp}
		case 'B': // Down arrow
			return InputResult{Action: ActionPaletteDown}
		}
	}
	return InputResult{Action: ActionNone}
}
