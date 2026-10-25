package agent

import (
	"testing"
	"time"

	"github.com/SujanPoudel1080/dajimux/internal/protocol"
)

func TestBrokerStatusAndMessaging(t *testing.T) {
	statusChanged := false
	broker := NewBroker(func(status *PaneStatus) {
		statusChanged = true
	})

	// Test Status Update
	status := broker.UpdateStatus("pane-1", protocol.AgentStatusPayload{
		State:    "thinking",
		Badge:    "🧠 Analyzing AST",
		Progress: 0.45,
	})

	if status.State != "thinking" {
		t.Fatalf("expected state 'thinking', got %s", status.State)
	}
	if !statusChanged {
		t.Fatalf("expected onStatusChange callback to fire")
	}

	fetched := broker.GetStatus("pane-1")
	if fetched == nil || fetched.Badge != "🧠 Analyzing AST" {
		t.Fatalf("expected fetched status badge to match")
	}

	// Test Direct Message delivery
	broker.Send("pane-1", "pane-2", "Hello from pane 1")
	inbox := broker.Receive("pane-2")
	if len(inbox) != 1 {
		t.Fatalf("expected 1 message in inbox, got %d", len(inbox))
	}
	if inbox[0].Content != "Hello from pane 1" {
		t.Fatalf("unexpected message content: %s", inbox[0].Content)
	}

	// Inbox should be empty after receiving
	inbox2 := broker.Receive("pane-2")
	if len(inbox2) != 0 {
		t.Fatalf("expected 0 messages on second receive, got %d", len(inbox2))
	}

	// Test Pub/Sub
	subCh := broker.Subscribe("pane-2", "#builds")
	broker.Send("pane-1", "#builds", "Build completed successfully")

	select {
	case msg := <-subCh:
		if msg.Content != "Build completed successfully" {
			t.Fatalf("unexpected pubsub content: %s", msg.Content)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for pubsub message")
	}

	broker.Unsubscribe("pane-2", "#builds")
	broker.RemovePane("pane-1")
	if broker.GetStatus("pane-1") != nil {
		t.Fatalf("expected pane-1 status to be removed")
	}
}
