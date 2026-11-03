package client

import (
	"bytes"
	"testing"
)

func TestANSICompositorCursorOffset(t *testing.T) {
	// Pane at col 1, row 2 (just below top chrome), 80x22
	comp := NewANSICompositor(1, 2, 80, 22)

	tests := []struct {
		name   string
		input  []byte
		expect []byte
	}{
		{
			name:   "home CSI H → row2 col1",
			input:  []byte("\033[H"),
			expect: []byte("\033[2;1H"),
		},
		{
			name:   "absolute row 1 col 1 → row2 col1",
			input:  []byte("\033[1;1H"),
			expect: []byte("\033[2;1H"),
		},
		{
			name:   "absolute row 5 col 10 → row6 col10",
			input:  []byte("\033[5;10H"),
			expect: []byte("\033[6;10H"),
		},
		{
			name:   "row beyond pane height clamped",
			input:  []byte("\033[100;1H"),
			expect: []byte("\033[23;1H"), // offY(2) + h(22) - 1 = 23
		},
		{
			name:   "CSI 2J erases pane, not full screen",
			input:  []byte("\033[2J"),
			expect: nil, // just check it doesn't contain \033[2J
		},
		{
			name:   "alt screen enter dropped",
			input:  []byte("\033[?1049h"),
			expect: []byte{},
		},
		{
			name:   "alt screen leave dropped",
			input:  []byte("\033[?1049l"),
			expect: []byte{},
		},
		{
			name:   "scroll region dropped",
			input:  []byte("\033[1;22r"),
			expect: []byte{},
		},
		{
			name:   "DECSC/DECRC dropped",
			input:  []byte("\0337\0338"),
			expect: []byte{},
		},
		{
			name:   "color SGR passed through unchanged",
			input:  []byte("\033[0;32;1m"),
			expect: []byte("\033[0;32;1m"),
		},
		{
			name:   "relative move A passed through",
			input:  []byte("\033[3A"),
			expect: []byte("\033[3A"),
		},
		{
			name:   "plain text passed through",
			input:  []byte("hello world"),
			expect: []byte("hello world"),
		},
		{
			name:   "OSC 0 title passed through",
			input:  []byte("\033]0;my title\a"),
			expect: []byte("\033]0;my title\a"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := comp.Rewrite(tt.input)
			if tt.expect == nil {
				// Just verify CSI 2J is not in output (was replaced)
				if bytes.Contains(got, []byte("\033[2J")) {
					t.Errorf("expected CSI 2J to be replaced, still present in output: %q", got)
				}
			} else {
				if !bytes.Equal(got, tt.expect) {
					t.Errorf("Rewrite(%q):\n  got  %q\n  want %q", tt.input, got, tt.expect)
				}
			}
		})
	}
}

func TestANSICompositorOffsetPaneAt40(t *testing.T) {
	// Right-hand pane starting at col 41, row 2, size 40x22
	comp := NewANSICompositor(41, 2, 40, 22)

	// Shell moves to row 1 col 1 (its home) → should land at row2 col41
	got := comp.Rewrite([]byte("\033[1;1H"))
	want := []byte("\033[2;41H")
	if !bytes.Equal(got, want) {
		t.Errorf("right-pane home: got %q, want %q", got, want)
	}

	// Col beyond pane width clamped to offX+w-1 = 80
	got = comp.Rewrite([]byte("\033[1;200H"))
	want = []byte("\033[2;80H")
	if !bytes.Equal(got, want) {
		t.Errorf("col clamp: got %q, want %q", got, want)
	}
}

func TestANSICompositorMixedOutput(t *testing.T) {
	comp := NewANSICompositor(1, 2, 80, 22)

	// Typical shell prompt sequence: color + cursor move + text
	input := []byte("\033[0m\033[32;1m\033[1;1Hsujan@mac ~ % \033[0m")
	got := comp.Rewrite(input)

	// Cursor move should be offset; colors should be untouched
	if !bytes.Contains(got, []byte("\033[2;1H")) {
		t.Errorf("expected offsetted cursor in output, got: %q", got)
	}
	if !bytes.Contains(got, []byte("\033[32;1m")) {
		t.Errorf("expected color sequence preserved in output, got: %q", got)
	}
	if !bytes.Contains(got, []byte("sujan@mac ~ % ")) {
		t.Errorf("expected text preserved in output, got: %q", got)
	}
}
