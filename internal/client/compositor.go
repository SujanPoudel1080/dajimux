package client

import (
	"bytes"
	"fmt"
	"strconv"
)

// ANSICompositor rewrites absolute cursor-positioning ANSI sequences and control
// characters emitted by a pane's PTY so that output stays strictly within the
// pane's viewport on the shared terminal screen.
//
// In particular:
//   - \r and \r\n are rewritten to jump to the pane's left column (offX), NOT
//     col 1 of the physical terminal (which would spill into the left pane).
//   - CSI H / CSI f (Cursor Position) are clamped and offset to (offX, offY).
//   - CSI G (CHA) is offset so col 1 lands at offX.
//   - CSI d (VPA) is offset so row 1 lands at offY.
//   - CSI K (Erase Line) uses CSI X (ECH) limited to the pane's width so it
//     never wipes across split borders into neighbor panes.
//   - Printable characters autowrap at the pane's inner width (w).
//   - CSI 2J / CSI J erases only the pane's viewport.
//   - Alt-screen enter/leave and scroll margins are suppressed.
type ANSICompositor struct {
	offX, offY int // pane origin in terminal coordinates (1-based)
	w, h       int // pane inner dimensions (excludes chrome/borders)
	curCol     int // 1-based relative to pane (1..w)
	curRow     int // 1-based relative to pane (1..h)
}

// NewANSICompositor creates a compositor for a pane placed at (offX, offY) with (w, h).
func NewANSICompositor(offX, offY, w, h int) *ANSICompositor {
	if offX < 1 {
		offX = 1
	}
	if offY < 1 {
		offY = 1
	}
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return &ANSICompositor{
		offX:   offX,
		offY:   offY,
		w:      w,
		h:      h,
		curCol: 1,
		curRow: 1,
	}
}

// Rewrite processes raw PTY output and returns a version that is strictly
// confined to this pane's rectangular screen region.
func (ac *ANSICompositor) Rewrite(input []byte) []byte {
	if len(input) == 0 {
		return input
	}

	out := make([]byte, 0, len(input)+64)

	// Ensure cursor starts at the pane's tracked coordinates before printing non-escape data
	if input[0] != 0x1b {
		out = append(out, fmt.Sprintf("\033[%d;%dH", ac.curRow+ac.offY-1, ac.curCol+ac.offX-1)...)
	}

	i := 0
	for i < len(input) {
		b := input[i]

		// 1. Carriage Return & Newline Handling
		// In a pane at (offX, offY), \r and \n must be explicitly positioned inside the pane.
		if b == '\r' {
			if i+1 < len(input) && input[i+1] == '\n' {
				// \r\n sequence: advance row and position at offX
				ac.curRow = clamp(ac.curRow+1, 1, ac.h)
				ac.curCol = 1
				out = append(out, fmt.Sprintf("\033[%d;%dH", ac.curRow+ac.offY-1, ac.offX)...)
				i += 2
				continue
			}
			// Standalone \r: reset to offX on current row
			ac.curCol = 1
			out = append(out, fmt.Sprintf("\033[%d;%dH", ac.curRow+ac.offY-1, ac.offX)...)
			i++
			continue
		}

		if b == '\n' {
			// Standalone \n: advance row and position at offX
			ac.curRow = clamp(ac.curRow+1, 1, ac.h)
			ac.curCol = 1
			out = append(out, fmt.Sprintf("\033[%d;%dH", ac.curRow+ac.offY-1, ac.offX)...)
			i++
			continue
		}

		// 2. Backspace Handling: prevent backspacing past pane's left edge
		if b == '\b' {
			if ac.curCol > 1 {
				ac.curCol--
				out = append(out, '\b')
			}
			i++
			continue
		}

		// 3. Tab Handling: advance to next 8-col tab stop within pane
		if b == '\t' {
			targetCol := ((ac.curCol-1)/8 + 1)*8 + 1
			if targetCol > ac.w {
				targetCol = ac.w
			}
			spaces := targetCol - ac.curCol
			for s := 0; s < spaces; s++ {
				out = append(out, ' ')
				ac.curCol++
			}
			i++
			continue
		}

		// 4. Regular Printable Characters (non-escape)
		if b != 0x1b {
			// Track column advancement for ASCII and UTF-8 leading bytes
			if b < 0x80 || b >= 0xC0 {
				if ac.curCol > ac.w {
					// Autowrap to next line inside pane boundaries
					ac.curRow = clamp(ac.curRow+1, 1, ac.h)
					ac.curCol = 1
					out = append(out, fmt.Sprintf("\033[%d;%dH", ac.curRow+ac.offY-1, ac.offX)...)
				}
				ac.curCol++
			}
			out = append(out, b)
			i++
			continue
		}

		// 5. ESC Sequence Processing
		if i+1 >= len(input) {
			out = append(out, b)
			i++
			continue
		}

		next := input[i+1]

		switch next {
		case '[': // CSI sequences
			// Find sequence terminator (letter or byte between 0x40 and 0x7E)
			j := i + 2
			for j < len(input) && (input[j] == ';' || input[j] == '?' || input[j] == ' ' || (input[j] >= '0' && input[j] <= '9')) {
				j++
			}
			if j >= len(input) {
				out = append(out, input[i:]...)
				i = len(input)
				continue
			}

			finalByte := input[j]
			params := string(input[i+2 : j])
			seqLen := j - i + 1

			switch finalByte {
			case 'H', 'f': // Cursor Position: CSI <r>;<c>H
				row, col := parseCursorPos(params)
				ac.curRow = clamp(row, 1, ac.h)
				ac.curCol = clamp(col, 1, ac.w)
				out = append(out, fmt.Sprintf("\033[%d;%dH", ac.curRow+ac.offY-1, ac.curCol+ac.offX-1)...)

			case 'G': // Cursor Horizontal Absolute: CSI <c>G
				col := 1
				if params != "" {
					if n, err := strconv.Atoi(params); err == nil && n > 0 {
						col = n
					}
				}
				ac.curCol = clamp(col, 1, ac.w)
				out = append(out, fmt.Sprintf("\033[%d;%dH", ac.curRow+ac.offY-1, ac.curCol+ac.offX-1)...)

			case 'd': // Cursor Vertical Absolute: CSI <r>d
				row := 1
				if params != "" {
					if n, err := strconv.Atoi(params); err == nil && n > 0 {
						row = n
					}
				}
				ac.curRow = clamp(row, 1, ac.h)
				out = append(out, fmt.Sprintf("\033[%d;%dH", ac.curRow+ac.offY-1, ac.curCol+ac.offX-1)...)

			case 'A': // Cursor Up: CSI <n>A (clamped to pane top)
				n := parseCount(params)
				ac.curRow = clamp(ac.curRow-n, 1, ac.h)
				out = append(out, fmt.Sprintf("\033[%d;%dH", ac.curRow+ac.offY-1, ac.curCol+ac.offX-1)...)

			case 'B': // Cursor Down: CSI <n>B (clamped to pane bottom)
				n := parseCount(params)
				ac.curRow = clamp(ac.curRow+n, 1, ac.h)
				out = append(out, fmt.Sprintf("\033[%d;%dH", ac.curRow+ac.offY-1, ac.curCol+ac.offX-1)...)

			case 'C': // Cursor Forward: CSI <n>C (clamped to pane right)
				n := parseCount(params)
				ac.curCol = clamp(ac.curCol+n, 1, ac.w)
				out = append(out, fmt.Sprintf("\033[%d;%dH", ac.curRow+ac.offY-1, ac.curCol+ac.offX-1)...)

			case 'D': // Cursor Backward: CSI <n>D (clamped to pane left)
				n := parseCount(params)
				ac.curCol = clamp(ac.curCol-n, 1, ac.w)
				out = append(out, fmt.Sprintf("\033[%d;%dH", ac.curRow+ac.offY-1, ac.curCol+ac.offX-1)...)

			case 'K': // Erase in Line: CSI <n>K
				// Standard \033[K erases all the way to the right edge of the physical
				// screen, destroying neighbor panes! Replace with ECH (\033[X) scoped
				// strictly to this pane's remaining width.
				if params == "" || params == "0" {
					// Erase from cursor to end of pane line
					rem := ac.w - ac.curCol + 1
					if rem > 0 {
						out = append(out, fmt.Sprintf("\033[%dX", rem)...)
					}
				} else if params == "1" {
					// Erase from start of pane line to cursor
					out = append(out, fmt.Sprintf("\033[%d;%dH\033[%dX\033[%d;%dH", ac.curRow+ac.offY-1, ac.offX, ac.curCol, ac.curRow+ac.offY-1, ac.offX+ac.curCol-1)...)
				} else if params == "2" {
					// Erase entire line within pane
					out = append(out, fmt.Sprintf("\033[%d;%dH\033[%dX\033[%d;%dH", ac.curRow+ac.offY-1, ac.offX, ac.w, ac.curRow+ac.offY-1, ac.offX+ac.curCol-1)...)
				}

			case 'X': // Erase Character: CSI <n>X
				n := parseCount(params)
				rem := ac.w - ac.curCol + 1
				if n > rem {
					n = rem
				}
				if n > 0 {
					out = append(out, fmt.Sprintf("\033[%dX", n)...)
				}

			case 'J': // Erase in Display: CSI <n>J
				if params == "2" {
					// 2: Erase entire pane viewport (e.g. from clear command)
					out = append(out, ac.erasePaneViewport()...)
					ac.curRow = 1
					ac.curCol = 1
				} else if params == "" || params == "0" {
					// 0 (default / clr_eos): Erase from cursor to bottom of pane without wiping lines above
					out = append(out, ac.eraseBelow()...)
				} else if params == "1" {
					// 1: Erase from top of pane to cursor
					out = append(out, ac.eraseAbove()...)
				}

			case 'r': // Set scrolling region — drop (we manage viewport globally)
				// discard

			case 'h', 'l': // DEC private mode set/reset
				if bytes.HasPrefix([]byte(params), []byte("?1049")) || bytes.HasPrefix([]byte(params), []byte("?47")) {
					// Alternate screen buffer: drop
				} else {
					out = append(out, input[i:i+seqLen]...)
				}

			default:
				// Pass all other CSI sequences through unchanged (SGR colors, cursor show/hide, etc.)
				out = append(out, input[i:i+seqLen]...)
			}

			i += seqLen

		case ']': // OSC sequences (clipboard, titles) — pass through
			j := i + 2
			for j < len(input) {
				if input[j] == '\a' {
					j++
					break
				}
				if input[j] == 0x1b && j+1 < len(input) && input[j+1] == '\\' {
					j += 2
					break
				}
				j++
			}
			out = append(out, input[i:j]...)
			i = j

		case '_': // APC (Kitty graphics) — pass through
			j := i + 2
			for j < len(input) {
				if input[j] == 0x1b && j+1 < len(input) && input[j+1] == '\\' {
					j += 2
					break
				}
				j++
			}
			out = append(out, input[i:j]...)
			i = j

		case '7', '8': // DECSC / DECRC save/restore cursor — drop
			i += 2

		case 'c': // Full terminal reset — drop
			i += 2

		default:
			out = append(out, b)
			i++
		}
	}

	return out
}

// erasePaneViewport returns ANSI sequences that clear only the pane's content area.
func (ac *ANSICompositor) erasePaneViewport() []byte {
	var b bytes.Buffer
	for row := ac.offY; row < ac.offY+ac.h; row++ {
		b.WriteString(fmt.Sprintf("\033[%d;%dH\033[%dX", row, ac.offX, ac.w))
	}
	b.WriteString(fmt.Sprintf("\033[%d;%dH", ac.offY, ac.offX))
	return b.Bytes()
}

// eraseBelow erases from the current cursor position to the bottom of the pane viewport
// without touching lines above the cursor or moving the cursor position.
func (ac *ANSICompositor) eraseBelow() []byte {
	var b bytes.Buffer
	// 1. Erase remaining characters on current row
	rem := ac.w - ac.curCol + 1
	if rem > 0 {
		b.WriteString(fmt.Sprintf("\033[%dX", rem))
	}
	// 2. Erase all rows below current row within pane bounds
	for r := ac.curRow + 1; r <= ac.h; r++ {
		rowTerm := r + ac.offY - 1
		b.WriteString(fmt.Sprintf("\033[%d;%dH\033[%dX", rowTerm, ac.offX, ac.w))
	}
	// 3. Restore cursor position
	b.WriteString(fmt.Sprintf("\033[%d;%dH", ac.curRow+ac.offY-1, ac.curCol+ac.offX-1))
	return b.Bytes()
}

// eraseAbove erases from the top of the pane to the current cursor position.
func (ac *ANSICompositor) eraseAbove() []byte {
	var b bytes.Buffer
	for r := 1; r < ac.curRow; r++ {
		rowTerm := r + ac.offY - 1
		b.WriteString(fmt.Sprintf("\033[%d;%dH\033[%dX", rowTerm, ac.offX, ac.w))
	}
	// Erase start of current row up to cursor
	if ac.curCol > 0 {
		b.WriteString(fmt.Sprintf("\033[%d;%dH\033[%dX", ac.curRow+ac.offY-1, ac.offX, ac.curCol))
	}
	// Restore cursor position
	b.WriteString(fmt.Sprintf("\033[%d;%dH", ac.curRow+ac.offY-1, ac.curCol+ac.offX-1))
	return b.Bytes()
}

// UpdateBounds updates the pane offset and dimensions while clamping cursor tracking.
func (ac *ANSICompositor) UpdateBounds(offX, offY, w, h int) {
	if offX < 1 {
		offX = 1
	}
	if offY < 1 {
		offY = 1
	}
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	ac.offX = offX
	ac.offY = offY
	ac.w = w
	ac.h = h
	ac.curCol = clamp(ac.curCol, 1, ac.w)
	ac.curRow = clamp(ac.curRow, 1, ac.h)
}

// parseCursorPos parses "row;col" or empty string (→ 1,1) from a CSI H/f param.
func parseCursorPos(params string) (row, col int) {
	row, col = 1, 1
	if params == "" {
		return
	}
	semi := bytes.IndexByte([]byte(params), ';')
	if semi < 0 {
		if n, err := strconv.Atoi(params); err == nil && n > 0 {
			row = n
		}
		return
	}
	if n, err := strconv.Atoi(params[:semi]); err == nil && n > 0 {
		row = n
	}
	if n, err := strconv.Atoi(params[semi+1:]); err == nil && n > 0 {
		col = n
	}
	return
}

func parseCount(params string) int {
	if params == "" {
		return 1
	}
	if n, err := strconv.Atoi(params); err == nil && n > 0 {
		return n
	}
	return 1
}

func clamp(val, lo, hi int) int {
	if val < lo {
		return lo
	}
	if val > hi {
		return hi
	}
	return val
}
