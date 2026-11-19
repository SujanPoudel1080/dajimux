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
	ad.handlers["split_h"] = func(s *Session, params map[string]string) error {
		_, err := s.SplitActive(bsp.SplitHorizontal)
		return err
	}

	ad.handlers["split_v"] = func(s *Session, params map[string]string) error {
		_, err := s.SplitActive(bsp.SplitVertical)
		return err
	}

	ad.handlers["split_auto"] = func(s *Session, params map[string]string) error {
		_, err := s.SplitActiveAuto()
		return err
	}

	ad.handlers["close_pane"] = func(s *Session, params map[string]string) error {
		if paneID := params["pane_id"]; paneID != "" {
			return s.ClosePaneByID(paneID)
		}
		return s.CloseActive()
	}

	ad.handlers["nav_left"] = func(s *Session, params map[string]string) error {
		s.Navigate(bsp.NavLeft)
		return nil
	}

	ad.handlers["nav_down"] = func(s *Session, params map[string]string) error {
		s.Navigate(bsp.NavDown)
		return nil
	}

	ad.handlers["nav_up"] = func(s *Session, params map[string]string) error {
		s.Navigate(bsp.NavUp)
		return nil
	}

	ad.handlers["nav_right"] = func(s *Session, params map[string]string) error {
		s.Navigate(bsp.NavRight)
		return nil
	}

	ad.handlers["focus_pane"] = func(s *Session, params map[string]string) error {
		if paneID := params["pane_id"]; paneID != "" {
			s.FocusPane(paneID)
		}
		return nil
	}

	ad.handlers["swap_next"] = func(s *Session, params map[string]string) error {
		return s.SwapActiveWithNext()
	}

	ad.handlers["swap_left"] = func(s *Session, params map[string]string) error {
		return s.SwapActive(bsp.NavLeft)
	}

	ad.handlers["swap_down"] = func(s *Session, params map[string]string) error {
		return s.SwapActive(bsp.NavDown)
	}

	ad.handlers["swap_up"] = func(s *Session, params map[string]string) error {
		return s.SwapActive(bsp.NavUp)
	}

	ad.handlers["swap_right"] = func(s *Session, params map[string]string) error {
		return s.SwapActive(bsp.NavRight)
	}

	ad.handlers["move_pane_ws"] = func(s *Session, params map[string]string) error {
		var wsNum int
		if fmt.Sscanf(params["workspace"], "%d", &wsNum); wsNum > 0 {
			return s.MoveActiveToWorkspace(wsNum)
		}
		return nil
	}

	ad.handlers["toggle_monocle"] = func(s *Session, params map[string]string) error {
		s.ToggleMonocle()
		return nil
	}

	ad.handlers["switch_workspace"] = func(s *Session, params map[string]string) error {
		var wsNum int
		if fmt.Sscanf(params["workspace"], "%d", &wsNum); wsNum > 0 {
			s.SwitchWorkspace(wsNum)
		}
		return nil
	}

	ad.handlers["resize_delta"] = func(s *Session, params map[string]string) error {
		var delta float64
		if fmt.Sscanf(params["delta"], "%f", &delta); delta != 0 {
			s.AdjustSplit(delta)
		}
		return nil
	}

	ad.handlers["mode_change"] = func(s *Session, params map[string]string) error {
		target := protocol.Mode(params["target"])
		if target != "" {
			s.SetMode(target)
		}
		return nil
	}

	ad.handlers["cycle_layout"] = func(s *Session, params map[string]string) error {
		s.CycleLayout()
		return nil
	}

	ad.handlers["set_layout"] = func(s *Session, params map[string]string) error {
		lt := bsp.LayoutType(params["type"])
		if lt != "" {
			s.SetLayout(lt)
		}
		return nil
	}

	ad.handlers["rename_workspace"] = func(s *Session, params map[string]string) error {
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

	ad.handlers["redraw"] = func(s *Session, params map[string]string) error {
		s.NotifyState()
		return nil
	}
}
