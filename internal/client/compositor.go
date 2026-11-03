package client

import (
	"bytes"
	"fmt"
	"strconv"
)

// ANSICompositor rewrites absolute cursor-positioning ANSI sequences emitted by
// a pane's PTY so that they land in the correct place on the shared terminal
// screen, accounting for the pane's origin (offX, offY) and size (w, h) and
// the chrome rows reserved at the top and bottom.
//
// Rules applied (CSI = \033[):
//   - CSI H / CSI f / CSI <r>;<c>H / CSI <r>;<c>f  → clamp+offset row/col
//   - CSI <n>A / CSI <n>B / CSI <n>C / CSI <n>D    → pass through unchanged
//     (relative moves are fine; the cursor stays inside the pane naturally)
//   - CSI 2J / CSI J  → replace with "erase pane viewport" instead of full-screen erase
//   - \033[?1049h / \033[?1049l (alt-screen enter/leave)              → drop
//   - \033[<r>r (set scrolling margins)                               → drop
//   - Everything else passes through unchanged.
type ANSICompositor struct {
	offX, offY int // pane origin in terminal coordinates (1-based)
	w, h       int // pane inner dimensions (excludes chrome)
}

// NewANSICompositor creates a compositor for a pane placed at (offX,offY) with (w,h).
// offY should be 2 (first row below the top status bar); offX is 1 for the leftmost pane.
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
	return &ANSICompositor{offX: offX, offY: offY, w: w, h: h}
}

// Rewrite processes raw PTY output and returns a version safe to write directly
// to the shared host terminal.
func (ac *ANSICompositor) Rewrite(input []byte) []byte {
	if len(input) == 0 {
		return input
	}

	out := make([]byte, 0, len(input)+32)
	i := 0

	for i < len(input) {
		b := input[i]

		if b != 0x1b {
			out = append(out, b)
			i++
			continue
		}

		// ESC sequence
		if i+1 >= len(input) {
			out = append(out, b)
			i++
			continue
		}

		next := input[i+1]

		switch next {
		case '[': // CSI sequences
			// Find the end of the CSI sequence (terminates on a letter)
			j := i + 2
			for j < len(input) && (input[j] == ';' || input[j] == '?' || (input[j] >= '0' && input[j] <= '9')) {
				j++
			}
			if j >= len(input) {
				// Incomplete sequence — pass through as-is
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
				// Offset into pane viewport
				row = clamp(row+ac.offY-1, ac.offY, ac.offY+ac.h-1)
				col = clamp(col+ac.offX-1, ac.offX, ac.offX+ac.w-1)
				out = append(out, fmt.Sprintf("\033[%d;%dH", row, col)...)

			case 'J': // Erase in Display
				// Replace CSI 2J (erase whole screen) with erase of pane viewport
				if params == "2" || params == "" {
					out = append(out, ac.erasePaneViewport()...)
				} else {
					out = append(out, input[i:i+seqLen]...)
				}

			case 'r': // Set scrolling region — drop; we own scrolling margins globally
				// discard

			case 'h', 'l': // DEC private mode set/reset
				if bytes.HasPrefix([]byte(params), []byte("?1049")) {
					// Alternate screen buffer: drop — we already manage this
				} else if bytes.HasPrefix([]byte(params), []byte("?47")) {
					// drop alternate screen
				} else {
					out = append(out, input[i:i+seqLen]...)
				}

			default:
				// Pass all other CSI sequences through unchanged (colors, attributes,
				// relative moves A/B/C/D, cursor show/hide, etc.)
				out = append(out, input[i:i+seqLen]...)
			}

			i += seqLen

		case ']': // OSC sequences — pass through (OSC 52 clipboard, OSC 0 title, etc.)
			// Find ST (BEL \a or ESC \)
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

		case '7', '8': // DECSC / DECRC save/restore cursor — drop (we manage cursor)
			i += 2

		case 'c': // Full terminal reset — drop
			i += 2

		default:
			// Unknown escape — pass through
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

// parseCursorPos parses "row;col" or empty string (→ 1,1) from a CSI H/f param.
func parseCursorPos(params string) (row, col int) {
	row, col = 1, 1
	if params == "" {
		return
	}
	semi := bytes.IndexByte([]byte(params), ';')
	if semi < 0 {
		if n, err := strconv.Atoi(params); err == nil {
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

func clamp(val, lo, hi int) int {
	if val < lo {
		return lo
	}
	if val > hi {
		return hi
	}
	return val
}
