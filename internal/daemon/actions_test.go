package daemon

import (
	"testing"

	"github.com/SujanPoudel1080/dajimux/internal/protocol"
)

func TestActionDispatcherDispatch(t *testing.T) {
	dispatcher := NewActionDispatcher()

	sess, err := NewSession("test-actions", 80, 24, nil)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	defer sess.Close()

	// 1. Test mode change
	err = dispatcher.Dispatch(sess, protocol.ActionPayload{
		Action: protocol.ActionModeChange,
		Params: map[string]string{"target": string(protocol.ModeNormal)},
	})
	if err != nil {
		t.Fatalf("unexpected error for mode_change: %v", err)
	}
	if sess.Mode != protocol.ModeNormal {
		t.Errorf("expected mode normal, got %s", sess.Mode)
	}

	// 2. Test cycle layout
	err = dispatcher.Dispatch(sess, protocol.ActionPayload{
		Action: protocol.ActionCycleLayout,
	})
	if err != nil {
		t.Fatalf("unexpected error for cycle_layout: %v", err)
	}

	// 3. Test rename workspace
	err = dispatcher.Dispatch(sess, protocol.ActionPayload{
		Action: protocol.ActionRenameWorkspace,
		Params: map[string]string{"name": "code"},
	})
	if err != nil {
		t.Fatalf("unexpected error for rename_workspace: %v", err)
	}
	if sess.Workspaces[sess.ActiveWS].Name != "code" {
		t.Errorf("expected workspace name 'code', got %q", sess.Workspaces[sess.ActiveWS].Name)
	}

	// 4. Test unknown action
	err = dispatcher.Dispatch(sess, protocol.ActionPayload{
		Action: "unknown_xyz",
	})
	if err == nil {
		t.Fatalf("expected error for unknown action, got nil")
	}

	// 5. Test nil session
	err = dispatcher.Dispatch(nil, protocol.ActionPayload{
		Action: protocol.ActionRedraw,
	})
	if err == nil {
		t.Fatalf("expected error for nil session, got nil")
	}
}

func TestActionDispatcherCustomRegistration(t *testing.T) {
	dispatcher := NewActionDispatcher()
	customCalled := false

	dispatcher.Register("custom_ping", func(s *Session, params map[string]string) error {
		customCalled = true
		return nil
	})

	sess, err := NewSession("test-custom", 80, 24, nil)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	defer sess.Close()

	err = dispatcher.Dispatch(sess, protocol.ActionPayload{Action: "custom_ping"})
	if err != nil {
		t.Fatalf("unexpected error for custom action: %v", err)
	}
	if !customCalled {
		t.Errorf("expected custom action handler to be called")
	}
}
