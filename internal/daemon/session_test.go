package daemon

import (
	"sync"
	"testing"
)

func TestSessionSummaryAndDetailedPanes(t *testing.T) {
	s, err := NewSession("test-summary", 80, 24, nil)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer s.Close()

	wsCount, panesCount, activeWS := s.Summary()
	if wsCount != 1 || panesCount != 1 || activeWS != 1 {
		t.Errorf("unexpected summary: ws=%d, panes=%d, active=%d", wsCount, panesCount, activeWS)
	}

	panes := s.DetailedPanes()
	if len(panes) != 1 {
		t.Fatalf("expected 1 detailed pane, got %d", len(panes))
	}
	if panes[0].SessionName != "test-summary" {
		t.Errorf("expected session name test-summary, got %s", panes[0].SessionName)
	}

	s.Rename("renamed-session")
	if s.Name != "renamed-session" {
		t.Errorf("expected session name renamed-session, got %s", s.Name)
	}
}

func TestSessionUpdatePaneAgentStatus(t *testing.T) {
	s, err := NewSession("test-agent-status", 80, 24, nil)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer s.Close()

	initialPane := s.ActivePane()
	if initialPane == nil {
		t.Fatalf("expected active pane to exist")
	}
	s.UpdatePaneAgentStatus(initialPane.ID, "thinking", "🧠 reasoning")

	_, state, badge, _ := initialPane.Snapshot()
	if state != "thinking" || badge != "🧠 reasoning" {
		t.Errorf("unexpected agent status: state=%s, badge=%s", state, badge)
	}
}

func TestConcurrentSessionAccess(t *testing.T) {
	s, err := NewSession("test-concurrency", 80, 24, nil)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer s.Close()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			_ = s.DetailedPanes()
		}()
		go func() {
			defer wg.Done()
			_, _, _ = s.Summary()
		}()
		go func() {
			defer wg.Done()
			_ = s.GetLayoutState()
		}()
	}
	wg.Wait()
}

func TestSessionTerminateOnLastPaneClose(t *testing.T) {
	s, err := NewSession("test-terminate", 80, 24, nil)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer s.Close()

	terminated := false
	var terminatedSession string
	s.OnTerminate = func(name string) {
		terminated = true
		terminatedSession = name
	}

	active := s.ActivePane()
	if active == nil {
		t.Fatalf("expected active pane to exist")
	}

	if err := s.ClosePaneByID(active.ID); err != nil {
		t.Fatalf("ClosePaneByID failed: %v", err)
	}

	if !terminated {
		t.Error("expected OnTerminate to be called when closing the last pane")
	}
	if terminatedSession != "test-terminate" {
		t.Errorf("expected terminated session name 'test-terminate', got %q", terminatedSession)
	}
	if !s.isClosed {
		t.Error("expected session to be marked closed")
	}
}
