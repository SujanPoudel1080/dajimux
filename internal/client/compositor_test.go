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
			name:   "relative move A clamped to pane top",
			input:  []byte("\033[3A"),
			expect: []byte("\033[2;1H"),
		},
		{
			name:   "plain text prefixed with pane cursor position",
			input:  []byte("hello world"),
			expect: []byte("\033[2;1Hhello world"),
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

func TestANSICompositorCarriageReturnAndNewlineInOffsetPane(t *testing.T) {
	// Right-hand pane starting at col 42, row 2, size 38x20
	comp := NewANSICompositor(42, 2, 38, 20)

	// Standalone \r should position cursor at row 2, col 42, NOT col 1 of the terminal!
	got := comp.Rewrite([]byte("hello\rworld"))
	if bytes.Contains(got, []byte("\r")) {
		t.Errorf("output should not contain raw \\r: got %q", got)
	}
	if !bytes.Contains(got, []byte("\033[2;42H")) {
		t.Errorf("expected \\033[2;42H in output for \\r, got: %q", got)
	}

	// \r\n should advance to row 3 and position at col 42
	comp2 := NewANSICompositor(42, 2, 38, 20)
	got2 := comp2.Rewrite([]byte("ls\r\nDocuments\tDownloads\r\n"))
	if bytes.Contains(got2, []byte("\r")) {
		t.Errorf("output should not contain raw \\r: got %q", got2)
	}
	if !bytes.Contains(got2, []byte("\033[3;42H")) {
		t.Errorf("expected \\033[3;42H in output for \\r\\n, got: %q", got2)
	}
}

func TestANSICompositorCSI_G_And_K(t *testing.T) {
	// Right-hand pane starting at col 42, row 2, size 38x20
	comp := NewANSICompositor(42, 2, 38, 20)

	// \033[1G (move to col 1 in pane) should become explicit position at row 2, col 42
	got := comp.Rewrite([]byte("\033[1G"))
	want := []byte("\033[2;42H")
	if !bytes.Equal(got, want) {
		t.Errorf("CSI G rewrite: got %q, want %q", got, want)
	}

	// \033[K (erase to end of line) should NOT emit raw \033[K (which wipes neighbor pane)
	comp2 := NewANSICompositor(2, 2, 38, 20)
	got2 := comp2.Rewrite([]byte("\033[K"))
	if bytes.Contains(got2, []byte("\033[K")) {
		t.Errorf("raw \\033[K should not be passed through: got %q", got2)
	}
	// At col 1 of 38-wide pane, it should erase remaining 38 chars: \033[38X
	if !bytes.Contains(got2, []byte("\033[38X")) {
		t.Errorf("expected \\033[38X in output for \\033[K, got: %q", got2)
	}
}

func TestANSICompositorEraseBelowPrompt(t *testing.T) {
	// Pane at (offX: 2, offY: 3), width 80, height 20
	comp := NewANSICompositor(2, 3, 80, 20)

	// Step 1: user runs ls, shell outputs directory names on row 1 and advances to row 2
	out1 := comp.Rewrite([]byte("Documents    Downloads    Projects\r\n"))
	if !bytes.Contains(out1, []byte("Documents    Downloads    Projects")) {
		t.Fatalf("expected directories in output, got: %q", out1)
	}
	if comp.curRow != 2 {
		t.Fatalf("expected curRow to be 2 after newline, got %d", comp.curRow)
	}

	// Step 2: shell prints prompt and emits \033[J (clear below, clr_eos)
	out2 := comp.Rewrite([]byte("\033[Juser@mac:~$ "))
	// \033[J must NOT reset curRow to 1, and must NOT clear line 1!
	if comp.curRow != 2 {
		t.Fatalf("expected curRow to remain 2 after \\033[J, got %d", comp.curRow)
	}
	// Prompt should be printed at terminal row 4 (offY 3 + curRow 2 - 1 = 4)
	if !bytes.Contains(out2, []byte("\033[4;2H")) {
		t.Fatalf("expected prompt positioned on row 4, got: %q", out2)
	}
	// Erase below must NOT erase row 3 (which is terminal row for pane line 1)
	if bytes.Contains(out2, []byte("\033[3;2H\033[80X")) {
		t.Fatalf("eraseBelow mistakenly cleared line 1 (row 3): %q", out2)
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
