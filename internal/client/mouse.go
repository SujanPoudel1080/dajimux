package client

import (
	"bytes"
	"strconv"
)

// MouseButton represents terminal mouse button codes.
type MouseButton int

const (
	MouseBtnLeft    MouseButton = 0
	MouseBtnMiddle  MouseButton = 1
	MouseBtnRight   MouseButton = 2
	MouseBtnRelease MouseButton = 3
	MouseWheelUp    MouseButton = 64
	MouseWheelDown  MouseButton = 65
)

// MouseEvent contains decoded SGR 1006 mouse event data.
type MouseEvent struct {
	Button MouseButton
	X      int  // 1-indexed column
	Y      int  // 1-indexed row
	IsDown bool // true for press, false for release
	IsDrag bool // true if dragging with button held
}

// ParseSGREvent parses an SGR 1006 sequence (e.g. \033[<0;20;10M or \033[<64;5;2M).
// Returns the parsed MouseEvent and true if successful.
func ParseSGREvent(data []byte) (*MouseEvent, bool) {
	if len(data) < 6 {
		return nil, false
	}
	if data[0] != 0x1b || data[1] != '[' || data[2] != '<' {
		return nil, false
	}

	lastChar := data[len(data)-1]
	if lastChar != 'M' && lastChar != 'm' {
		return nil, false
	}

	payload := string(data[3 : len(data)-1])
	parts := bytes.Split([]byte(payload), []byte{';'})
	if len(parts) != 3 {
		return nil, false
	}

	btnCode, err1 := strconv.Atoi(string(parts[0]))
	x, err2 := strconv.Atoi(string(parts[1]))
	y, err3 := strconv.Atoi(string(parts[2]))
	if err1 != nil || err2 != nil || err3 != nil {
		return nil, false
	}

	isDrag := (btnCode & 32) != 0
	isDown := (lastChar == 'M')
	rawBtn := btnCode &^ 32 // Strip drag bit for base button

	return &MouseEvent{
		Button: MouseButton(rawBtn),
		X:      x,
		Y:      y,
		IsDown: isDown,
		IsDrag: isDrag,
	}, true
}
