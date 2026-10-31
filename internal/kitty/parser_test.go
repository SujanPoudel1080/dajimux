package kitty

import (
	"bytes"
	"strings"
	"testing"
)

func TestKittyParserAndVirtualizer(t *testing.T) {
	parser := NewParser()

	// Sample stream containing normal text and a Kitty graphics sequence
	input := []byte("Hello Terminal!\x1b_Ga=T,f=100,s=20,v=10,c=5,r=2;iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==\x1b\\Goodbye!")

	passthrough, commands := parser.ParseChunk(input)

	// Verify text passthrough
	expectedText := "Hello Terminal!Goodbye!"
	if string(passthrough) != expectedText {
		t.Fatalf("expected passthrough %q, got %q", expectedText, string(passthrough))
	}

	// Verify command extraction
	if len(commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(commands))
	}

	cmd := commands[0]
	if cmd.Control["a"] != "T" || cmd.Control["f"] != "100" {
		t.Errorf("control attributes mismatch: %+v", cmd.Control)
	}
	if !bytes.HasPrefix(cmd.Payload, []byte("iVBORw0KGgo")) {
		t.Errorf("payload mismatch: %s", string(cmd.Payload))
	}

	// Test Virtualization: offset to Pane at (X=30, Y=10)
	virt := NewVirtualizer("pane-1")
	translated := virt.Translate(&cmd, 30, 10, 40, 20)

	transStr := string(translated)
	// Original c=5 -> should become c=35
	if !strings.Contains(transStr, "c=35") {
		t.Errorf("expected translated c=35 in %s", transStr)
	}
	// Original r=2 -> should become r=12
	if !strings.Contains(transStr, "r=12") {
		t.Errorf("expected translated r=12 in %s", transStr)
	}
}
