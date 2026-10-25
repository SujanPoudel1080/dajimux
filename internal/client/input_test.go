package client

import (
	"testing"
	"time"

	"github.com/SujanPoudel1080/dajimux/internal/protocol"
)

func TestInputHandlerDoubleTapSpace(t *testing.T) {
	handler := NewInputHandler()

	if handler.CurrentMode != protocol.ModeInsert {
		t.Fatalf("expected initial mode to be INSERT, got %s", handler.CurrentMode)
	}

	// First Space tap
	res1 := handler.HandleKey([]byte{' '})
	if res1.Action != ActionPassThrough {
		t.Fatalf("expected first space to passthrough, got %s", res1.Action)
	}
	if string(res1.RawForward) != " " {
		t.Fatalf("expected space byte forwarded, got %v", res1.RawForward)
	}
	if handler.CurrentMode != protocol.ModeInsert {
		t.Fatalf("expected mode to remain INSERT after first space, got %s", handler.CurrentMode)
	}

	// Immediate second Space tap (< 280ms)
	res2 := handler.HandleKey([]byte{' '})
	if res2.Action != ActionModeChange {
		t.Fatalf("expected double-tap to trigger mode change, got %s", res2.Action)
	}
	if res2.TargetMode != protocol.ModeNormal {
		t.Fatalf("expected target mode NORMAL, got %s", res2.TargetMode)
	}
	if len(res2.UndoBytes) == 0 || res2.UndoBytes[0] != 0x7f {
		t.Fatalf("expected backspace in UndoBytes, got %v", res2.UndoBytes)
	}
	if handler.CurrentMode != protocol.ModeNormal {
		t.Fatalf("expected mode to become NORMAL, got %s", handler.CurrentMode)
	}

	// In Normal mode, pressing 'v' should trigger split vertical
	res3 := handler.HandleKey([]byte{'v'})
	if res3.Action != ActionSplitVertical {
		t.Fatalf("expected ActionSplitVertical on 'v', got %s", res3.Action)
	}

	// Pressing 'i' in Normal mode should return to INSERT
	res4 := handler.HandleKey([]byte{'i'})
	if res4.Action != ActionModeChange || res4.TargetMode != protocol.ModeInsert {
		t.Fatalf("expected switch back to INSERT on 'i', got %s", res4.Action)
	}
	if handler.CurrentMode != protocol.ModeInsert {
		t.Fatalf("expected mode to be INSERT, got %s", handler.CurrentMode)
	}
}

func TestInputHandlerSlowDoubleSpace(t *testing.T) {
	handler := NewInputHandler()

	// First Space tap
	res1 := handler.HandleKey([]byte{' '})
	if res1.Action != ActionPassThrough {
		t.Fatalf("expected space to passthrough, got %s", res1.Action)
	}

	// Wait longer than the 280ms threshold
	time.Sleep(300 * time.Millisecond)

	// Second Space tap
	res2 := handler.HandleKey([]byte{' '})
	if res2.Action != ActionPassThrough {
		t.Fatalf("expected second slow space to also passthrough, got %s", res2.Action)
	}
	if handler.CurrentMode != protocol.ModeInsert {
		t.Fatalf("mode should still be INSERT, got %s", handler.CurrentMode)
	}
}
