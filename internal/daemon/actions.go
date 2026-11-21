package daemon

import (
	"fmt"

	"github.com/SujanPoudel1080/dajimux/internal/bsp"
	"github.com/SujanPoudel1080/dajimux/internal/protocol"
)

// ActionHandler defines a function that executes an action on a target session.
type ActionHandler func(s *Session, params map[string]string) error

// ActionDispatcher maps action names to their corresponding handlers.
type ActionDispatcher struct {
	handlers map[string]ActionHandler
}

// NewActionDispatcher creates a new dispatcher preloaded with all standard action handlers.
func NewActionDispatcher() *ActionDispatcher {
	ad := &ActionDispatcher{
		handlers: make(map[string]ActionHandler),
	}
	ad.registerDefaults()
	return ad
}

// Register registers a handler for a given action name.
func (ad *ActionDispatcher) Register(action string, handler ActionHandler) {
	ad.handlers[action] = handler
}

// Dispatch executes the handler associated with the specified action payload.
func (ad *ActionDispatcher) Dispatch(s *Session, action protocol.ActionPayload) error {
	if s == nil {
		return fmt.Errorf("session cannot be nil")
	}
	handler, ok := ad.handlers[action.Action]
	if !ok {
		return fmt.Errorf("unknown action: %s", action.Action)
	}
	return handler(s, action.Params)
}

func (ad *ActionDispatcher) registerDefaults() {
	ad.handlers[protocol.ActionSplitH] = func(s *Session, params map[string]string) error {
		_, err := s.SplitActive(bsp.SplitHorizontal)
		return err
	}

	ad.handlers[protocol.ActionSplitV] = func(s *Session, params map[string]string) error {
		_, err := s.SplitActive(bsp.SplitVertical)
		return err
	}

	ad.handlers[protocol.ActionSplitAuto] = func(s *Session, params map[string]string) error {
		_, err := s.SplitActiveAuto()
		return err
	}

	ad.handlers[protocol.ActionClosePane] = func(s *Session, params map[string]string) error {
		if paneID := params["pane_id"]; paneID != "" {
			return s.ClosePaneByID(paneID)
		}
		return s.CloseActive()
	}

	ad.handlers[protocol.ActionNavLeft] = func(s *Session, params map[string]string) error {
		s.Navigate(bsp.NavLeft)
		return nil
	}

	ad.handlers[protocol.ActionNavDown] = func(s *Session, params map[string]string) error {
		s.Navigate(bsp.NavDown)
		return nil
	}

	ad.handlers[protocol.ActionNavUp] = func(s *Session, params map[string]string) error {
		s.Navigate(bsp.NavUp)
		return nil
	}

	ad.handlers[protocol.ActionNavRight] = func(s *Session, params map[string]string) error {
		s.Navigate(bsp.NavRight)
		return nil
	}

	ad.handlers[protocol.ActionFocusPane] = func(s *Session, params map[string]string) error {
		if paneID := params["pane_id"]; paneID != "" {
			s.FocusPane(paneID)
		}
		return nil
	}

	ad.handlers[protocol.ActionSwapNext] = func(s *Session, params map[string]string) error {
		return s.SwapActiveWithNext()
	}

	ad.handlers[protocol.ActionSwapLeft] = func(s *Session, params map[string]string) error {
		return s.SwapActive(bsp.NavLeft)
	}

	ad.handlers[protocol.ActionSwapDown] = func(s *Session, params map[string]string) error {
		return s.SwapActive(bsp.NavDown)
	}

	ad.handlers[protocol.ActionSwapUp] = func(s *Session, params map[string]string) error {
		return s.SwapActive(bsp.NavUp)
	}

	ad.handlers[protocol.ActionSwapRight] = func(s *Session, params map[string]string) error {
		return s.SwapActive(bsp.NavRight)
	}

	ad.handlers[protocol.ActionMovePaneWS] = func(s *Session, params map[string]string) error {
		var wsNum int
		if fmt.Sscanf(params["workspace"], "%d", &wsNum); wsNum > 0 {
			return s.MoveActiveToWorkspace(wsNum)
		}
		return nil
	}

	ad.handlers[protocol.ActionToggleMonocle] = func(s *Session, params map[string]string) error {
		s.ToggleMonocle()
		return nil
	}

	ad.handlers[protocol.ActionSwitchWorkspace] = func(s *Session, params map[string]string) error {
		var wsNum int
		if fmt.Sscanf(params["workspace"], "%d", &wsNum); wsNum > 0 {
			s.SwitchWorkspace(wsNum)
		}
		return nil
	}

	ad.handlers[protocol.ActionResizeDelta] = func(s *Session, params map[string]string) error {
		var delta float64
		if fmt.Sscanf(params["delta"], "%f", &delta); delta != 0 {
			s.AdjustSplit(delta)
		}
		return nil
	}

	ad.handlers[protocol.ActionModeChange] = func(s *Session, params map[string]string) error {
		target := protocol.Mode(params["target"])
		if target != "" {
			s.SetMode(target)
		}
		return nil
	}

	ad.handlers[protocol.ActionCycleLayout] = func(s *Session, params map[string]string) error {
		s.CycleLayout()
		return nil
	}

	ad.handlers[protocol.ActionSetLayout] = func(s *Session, params map[string]string) error {
		lt := bsp.LayoutType(params["type"])
		if lt != "" {
			s.SetLayout(lt)
		}
		return nil
	}

	ad.handlers[protocol.ActionRenameWorkspace] = func(s *Session, params map[string]string) error {
		name := params["name"]
		if name != "" {
			wsNum := s.ActiveWS
			if params["workspace"] != "" {
				_, _ = fmt.Sscanf(params["workspace"], "%d", &wsNum)
			}
			s.RenameWorkspace(wsNum, name)
		}
		return nil
	}

	ad.handlers[protocol.ActionRedraw] = func(s *Session, params map[string]string) error {
		s.NotifyState()
		return nil
	}
}
