package daemon

import (
	"testing"
)

func TestExtractOSCTitle(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "OSC 0 with BEL",
			input:    []byte("\033]0;vim main.go\a"),
			expected: "vim main.go",
		},
		{
			name:     "OSC 2 with BEL",
			input:    []byte("\033]2;sujan@air:~/dajimux\a"),
			expected: "sujan@air:~/dajimux",
		},
		{
			name:     "OSC 0 with String Terminator (ST)",
			input:    []byte("\033]0;python app.py\033\\"),
			expected: "python app.py",
		},
		{
			name:     "Embedded in output stream",
			input:    []byte("hello world\033]0;htop\asome output"),
			expected: "htop",
		},
		{
			name:     "No OSC sequence",
			input:    []byte("regular terminal text\r\n"),
			expected: "",
		},
		{
			name:     "Empty title",
			input:    []byte("\033]0;\a"),
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := extractOSCTitle(tc.input)
			if actual != tc.expected {
				t.Errorf("extractOSCTitle() = %q, expected %q", actual, tc.expected)
			}
		})
	}
}

func TestPaneTitleChangeCallback(t *testing.T) {
	pane := NewPane("p1", "shell")
	var capturedID, capturedTitle string
	pane.SetOnTitleChange(func(id, title string) {
		capturedID = id
		capturedTitle = title
	})

	if pane.onTitleChange == nil {
		t.Fatalf("expected onTitleChange callback to be set")
	}

	pane.onTitleChange("p1", "nvim")
	if capturedID != "p1" || capturedTitle != "nvim" {
		t.Errorf("unexpected callback result: id=%q title=%q", capturedID, capturedTitle)
	}
}
