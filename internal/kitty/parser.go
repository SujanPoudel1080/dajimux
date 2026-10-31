package kitty

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// Command represents a parsed Kitty graphics protocol escape sequence.
type Command struct {
	Control map[string]string // Key-value parameters from the control string (e.g. a=T, f=100, s=100, v=100)
	Payload []byte            // Base64 or direct payload data
}

// Parser intercepts Kitty graphics commands (\x1b_G ... \x1b\ or \x07) from a byte stream.
type Parser struct {
	inSequence bool
	buf        bytes.Buffer
}

// NewParser creates a new Kitty graphics protocol parser.
func NewParser() *Parser {
	return &Parser{}
}

// ParseChunk scans input data and extracts any complete Kitty graphics sequences.
// Non-graphics data is returned in rawPassthrough.
func (p *Parser) ParseChunk(data []byte) (rawPassthrough []byte, commands []Command) {
	var out bytes.Buffer
	i := 0
	n := len(data)

	for i < n {
		if !p.inSequence {
			// Look for \x1b_G sequence start
			if i+2 < n && data[i] == 0x1b && data[i+1] == '_' && data[i+2] == 'G' {
				p.inSequence = true
				p.buf.Reset()
				p.buf.WriteString("\x1b_G")
				i += 3
				continue
			}
			out.WriteByte(data[i])
			i++
		} else {
			// In sequence, look for terminator \x1b\ (ST) or \x07 (BEL)
			if data[i] == 0x07 { // BEL
				p.buf.WriteByte(data[i])
				p.inSequence = false
				cmd := parseCommand(p.buf.Bytes())
				if cmd != nil {
					commands = append(commands, *cmd)
				}
				p.buf.Reset()
				i++
				continue
			}
			if i+1 < n && data[i] == 0x1b && data[i+1] == '\\' { // ST
				p.buf.WriteByte(data[i])
				p.buf.WriteByte(data[i+1])
				p.inSequence = false
				cmd := parseCommand(p.buf.Bytes())
				if cmd != nil {
					commands = append(commands, *cmd)
				}
				p.buf.Reset()
				i += 2
				continue
			}

			p.buf.WriteByte(data[i])
			i++
		}
	}

	return out.Bytes(), commands
}

func parseCommand(raw []byte) *Command {
	str := string(raw)
	// Strip \x1b_G prefix and terminator (\x07 or \x1b\)
	if !strings.HasPrefix(str, "\x1b_G") {
		return nil
	}
	str = strings.TrimPrefix(str, "\x1b_G")
	if strings.HasSuffix(str, "\x1b\\") {
		str = strings.TrimSuffix(str, "\x1b\\")
	} else if strings.HasSuffix(str, "\x07") {
		str = strings.TrimSuffix(str, "\x07")
	}

	parts := strings.SplitN(str, ";", 2)
	controlStr := parts[0]
	var payload []byte
	if len(parts) > 1 {
		payload = []byte(parts[1])
	}

	control := make(map[string]string)
	for _, kv := range strings.Split(controlStr, ",") {
		if kv == "" {
			continue
		}
		eq := strings.IndexByte(kv, '=')
		if eq > 0 {
			control[kv[:eq]] = kv[eq+1:]
		} else {
			control[kv] = "1"
		}
	}

	return &Command{
		Control: control,
		Payload: payload,
	}
}

// Virtualizer translates Kitty graphics coordinates relative to a pane into global terminal coordinates.
type Virtualizer struct {
	PaneID string
}

// NewVirtualizer creates a coordinator for a specific pane.
func NewVirtualizer(paneID string) *Virtualizer {
	return &Virtualizer{PaneID: paneID}
}

// Translate adjusts image placement parameters (c, r, X, Y) to fit within pane bounds on the host screen.
func (v *Virtualizer) Translate(cmd *Command, paneX, paneY, paneW, paneH int) []byte {
	// Offset column coordinate 'C' or 'c'
	if cStr, ok := cmd.Control["C"]; ok {
		if c, err := strconv.Atoi(cStr); err == nil {
			cmd.Control["C"] = strconv.Itoa(paneX + c)
		}
	} else if cStr, ok := cmd.Control["c"]; ok {
		if c, err := strconv.Atoi(cStr); err == nil {
			cmd.Control["c"] = strconv.Itoa(paneX + c)
		}
	} else {
		// If no column placement specified, pin to pane origin
		cmd.Control["c"] = strconv.Itoa(paneX)
	}

	// Offset row coordinate 'R' or 'r'
	if rStr, ok := cmd.Control["R"]; ok {
		if r, err := strconv.Atoi(rStr); err == nil {
			cmd.Control["R"] = strconv.Itoa(paneY + r)
		}
	} else if rStr, ok := cmd.Control["r"]; ok {
		if r, err := strconv.Atoi(rStr); err == nil {
			cmd.Control["r"] = strconv.Itoa(paneY + r)
		}
	} else {
		cmd.Control["r"] = strconv.Itoa(paneY)
	}

	// Re-encode command
	var controlPairs []string
	for k, val := range cmd.Control {
		controlPairs = append(controlPairs, fmt.Sprintf("%s=%s", k, val))
	}

	var out bytes.Buffer
	out.WriteString("\x1b_G")
	out.WriteString(strings.Join(controlPairs, ","))
	if len(cmd.Payload) > 0 {
		out.WriteString(";")
		out.Write(cmd.Payload)
	}
	out.WriteString("\x1b\\")

	return out.Bytes()
}
