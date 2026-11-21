package daemon

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/SujanPoudel1080/dajimux/internal/bsp"
	"github.com/SujanPoudel1080/dajimux/internal/protocol"
)

func (d *Daemon) handleAttachMsg(conn net.Conn, writer *protocol.EnvelopeWriter, env *protocol.Envelope) (*Session, error) {
	var p protocol.AttachPayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return nil, err
	}
	sessName := p.SessionName
	if sessName == "" {
		sessName = d.DefaultSession
	}
	session, err := d.GetOrCreateSession(sessName, p.Cols, p.Rows)
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	d.clients[conn] = &clientEntry{
		session: session,
		writer:  writer,
		cols:    p.Cols,
		rows:    p.Rows,
	}
	d.mu.Unlock()

	// Clamp session dimensions to minimum across all attached clients
	d.updateSessionDimensions(session)

	// Send current layout state to newly attached client
	state := session.GetLayoutState()
	data, _ := json.Marshal(state)
	_ = writer.Write(protocol.Envelope{
		Type:    protocol.MsgLayoutState,
		Payload: data,
	})

	// Forward PTY output from active pane to this client
	go d.streamPaneOutput(writer, session)
	return session, nil
}

func (d *Daemon) handleInputMsg(attachedSession *Session, env *protocol.Envelope) {
	var p protocol.InputPayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return
	}
	if env.PaneID != "" {
		d.mu.RLock()
		var targetPane *Pane
		for _, s := range d.Sessions {
			if pane := s.GetPane(env.PaneID); pane != nil {
				targetPane = pane
				break
			}
		}
		d.mu.RUnlock()
		if targetPane != nil {
			_, _ = targetPane.Write(p.Data)
		}
	} else if attachedSession != nil {
		if active := attachedSession.ActivePane(); active != nil {
			_, _ = active.Write(p.Data)
		}
	}
}

func (d *Daemon) handleResizeMsg(conn net.Conn, attachedSession *Session, env *protocol.Envelope) {
	if attachedSession == nil {
		return
	}
	var p protocol.ResizePayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return
	}
	d.mu.Lock()
	if entry, ok := d.clients[conn]; ok {
		entry.cols = p.Cols
		entry.rows = p.Rows
	}
	d.mu.Unlock()
	d.updateSessionDimensions(attachedSession)
}

func (d *Daemon) handleAgentStatusMsg(attachedSession *Session, env *protocol.Envelope) {
	sess := attachedSession
	if sess == nil {
		sess = d.getSessionForRequest(env.SessionID)
	}
	if sess == nil {
		return
	}
	var p protocol.AgentStatusPayload
	if err := json.Unmarshal(env.Payload, &p); err == nil {
		sess.Broker.UpdateStatus(env.PaneID, p)
	}
}

func (d *Daemon) handleAgentSendMsg(attachedSession *Session, writer *protocol.EnvelopeWriter, env *protocol.Envelope) {
	sess := attachedSession
	if sess == nil {
		sess = d.getSessionForRequest(env.SessionID)
	}
	if sess == nil {
		return
	}
	var p protocol.AgentMessagePayload
	if err := json.Unmarshal(env.Payload, &p); err == nil {
		msg := sess.Broker.Send(p.FromPaneID, p.ToTarget, p.Content)
		data, _ := json.Marshal(msg)
		_ = writer.Write(protocol.Envelope{
			Type:    protocol.MsgAgentSend,
			Payload: data,
		})
	}
}

func (d *Daemon) handlePaneCaptureMsg(attachedSession *Session, writer *protocol.EnvelopeWriter, env *protocol.Envelope) {
	sess := attachedSession
	if sess == nil {
		sess = d.getSessionForRequest(env.SessionID)
	}
	if sess == nil {
		return
	}
	var p protocol.PaneCaptureRequest
	if err := json.Unmarshal(env.Payload, &p); err == nil {
		sess.mu.RLock()
		targetPane := sess.Panes[p.TargetPaneID]
		sess.mu.RUnlock()

		var lines []string
		if targetPane != nil {
			lines = targetPane.CaptureLastLines(p.Lines)
		}
		res := protocol.PaneCaptureResponse{
			TargetPaneID: p.TargetPaneID,
			Lines:        lines,
		}
		data, _ := json.Marshal(res)
		_ = writer.Write(protocol.Envelope{
			Type:    protocol.MsgPaneCaptureRes,
			Payload: data,
		})
	}
}

func (d *Daemon) handleListSessionsMsg(writer *protocol.EnvelopeWriter) {
	d.mu.RLock()
	var summaries []protocol.SessionSummary
	for name, sess := range d.Sessions {
		sess.mu.RLock()
		wsCount := len(sess.Workspaces)
		panesCount := len(sess.Panes)
		activeWS := sess.ActiveWS
		sess.mu.RUnlock()

		clientCount := 0
		for _, entry := range d.clients {
			if entry.session != nil && entry.session.Name == name {
				clientCount++
			}
		}

		summaries = append(summaries, protocol.SessionSummary{
			Name:            name,
			WorkspacesCount: wsCount,
			PanesCount:      panesCount,
			ActiveWorkspace: activeWS,
			AttachedClients: clientCount,
		})
	}
	d.mu.RUnlock()

	resData, _ := json.Marshal(protocol.ListSessionsResponse{Sessions: summaries})
	_ = writer.Write(protocol.Envelope{
		Type:    protocol.MsgListSessionsRes,
		Payload: resData,
	})
}

func (d *Daemon) handleListPanesMsg(writer *protocol.EnvelopeWriter, env *protocol.Envelope) {
	var req protocol.ListPanesRequest
	_ = json.Unmarshal(env.Payload, &req)

	d.mu.RLock()
	var panes []protocol.DetailedPaneInfo
	for name, sess := range d.Sessions {
		if req.SessionName != "" && req.SessionName != name {
			continue
		}
		sess.mu.RLock()
		for wsID, ws := range sess.Workspaces {
			activePaneID := ws.ActivePaneID()
			for _, pid := range ws.LeafPanes() {
				rect, _, _, ok := ws.PaneGeometry(pid)
				pane := sess.Panes[pid]
				if ok && pane != nil {
					pane.mu.RLock()
					panes = append(panes, protocol.DetailedPaneInfo{
						SessionName: name,
						WorkspaceID: wsID,
						PaneID:      pid,
						Title:       pane.Title,
						Cwd:         pane.Cwd,
						IsActive:    pid == activePaneID && sess.ActiveWS == wsID,
						AgentState:  pane.AgentState,
						AgentBadge:  pane.AgentBadge,
						Width:       rect.W,
						Height:      rect.H,
					})
					pane.mu.RUnlock()
				}
			}
		}
		sess.mu.RUnlock()
	}
	d.mu.RUnlock()

	resData, _ := json.Marshal(protocol.ListPanesResponse{Panes: panes})
	_ = writer.Write(protocol.Envelope{
		Type:    protocol.MsgListPanesRes,
		Payload: resData,
	})
}

func (d *Daemon) handleSpawnPaneMsg(writer *protocol.EnvelopeWriter, env *protocol.Envelope) {
	var req protocol.SpawnPaneRequest
	_ = json.Unmarshal(env.Payload, &req)

	sessionName := req.SessionName
	if sessionName == "" {
		sessionName = "main"
	}

	d.mu.RLock()
	sess := d.Sessions[sessionName]
	d.mu.RUnlock()

	if sess == nil {
		resData, _ := json.Marshal(protocol.SpawnPaneResponse{
			Success: false,
			Error:   fmt.Sprintf("session %q not found", sessionName),
		})
		_ = writer.Write(protocol.Envelope{
			Type:    protocol.MsgSpawnPaneRes,
			Payload: resData,
		})
		return
	}

	dir := bsp.SplitVertical
	if req.Direction == "horizontal" {
		dir = bsp.SplitHorizontal
	}

	newPane, err := sess.SpawnPane(dir, req.Cwd, req.Command)
	if err != nil {
		resData, _ := json.Marshal(protocol.SpawnPaneResponse{
			Success: false,
			Error:   err.Error(),
		})
		_ = writer.Write(protocol.Envelope{
			Type:    protocol.MsgSpawnPaneRes,
			Payload: resData,
		})
	} else {
		resData, _ := json.Marshal(protocol.SpawnPaneResponse{
			Success: true,
			PaneID:  newPane.ID,
		})
		_ = writer.Write(protocol.Envelope{
			Type:    protocol.MsgSpawnPaneRes,
			Payload: resData,
		})
	}
}

func (d *Daemon) handleClosePaneMsg(writer *protocol.EnvelopeWriter, env *protocol.Envelope) {
	var req protocol.ClosePaneRequest
	_ = json.Unmarshal(env.Payload, &req)

	if req.PaneID == "" {
		resData, _ := json.Marshal(protocol.ClosePaneResponse{
			Success: false,
			Error:   "pane_id is required",
		})
		_ = writer.Write(protocol.Envelope{
			Type:    protocol.MsgClosePaneRes,
			Payload: resData,
		})
		return
	}

	err := d.ClosePaneByID(req.PaneID)
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	resData, _ := json.Marshal(protocol.ClosePaneResponse{
		Success: err == nil,
		Error:   errStr,
	})
	_ = writer.Write(protocol.Envelope{
		Type:    protocol.MsgClosePaneRes,
		Payload: resData,
	})
}

func (d *Daemon) handleKillSessionMsg(writer *protocol.EnvelopeWriter, env *protocol.Envelope) {
	var req protocol.KillSessionRequest
	_ = json.Unmarshal(env.Payload, &req)

	if req.SessionName == "" {
		resData, _ := json.Marshal(protocol.KillSessionResponse{
			Success: false,
			Error:   "session_name is required",
		})
		_ = writer.Write(protocol.Envelope{
			Type:    protocol.MsgKillSessionRes,
			Payload: resData,
		})
		return
	}

	err := d.KillSession(req.SessionName)
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	resData, _ := json.Marshal(protocol.KillSessionResponse{
		Success: err == nil,
		Error:   errStr,
	})
	_ = writer.Write(protocol.Envelope{
		Type:    protocol.MsgKillSessionRes,
		Payload: resData,
	})
}

func (d *Daemon) handleRenameSessionMsg(writer *protocol.EnvelopeWriter, env *protocol.Envelope) {
	var req protocol.RenameSessionRequest
	_ = json.Unmarshal(env.Payload, &req)

	if req.OldName == "" || req.NewName == "" {
		resData, _ := json.Marshal(protocol.RenameSessionResponse{
			Success: false,
			Error:   "old_name and new_name are required",
		})
		_ = writer.Write(protocol.Envelope{
			Type:    protocol.MsgRenameSessionRes,
			Payload: resData,
		})
		return
	}

	err := d.RenameSession(req.OldName, req.NewName)
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	resData, _ := json.Marshal(protocol.RenameSessionResponse{
		Success: err == nil,
		Error:   errStr,
	})
	_ = writer.Write(protocol.Envelope{
		Type:    protocol.MsgRenameSessionRes,
		Payload: resData,
	})
}

func (d *Daemon) handleRenameWorkspaceMsg(writer *protocol.EnvelopeWriter, env *protocol.Envelope) {
	var req protocol.RenameWorkspaceRequest
	_ = json.Unmarshal(env.Payload, &req)

	if req.NewName == "" {
		resData, _ := json.Marshal(protocol.RenameWorkspaceResponse{
			Success: false,
			Error:   "new_name is required",
		})
		_ = writer.Write(protocol.Envelope{
			Type:    protocol.MsgRenameWorkspaceRes,
			Payload: resData,
		})
		return
	}

	err := d.RenameWorkspace(req.SessionName, req.WorkspaceID, req.NewName)
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	resData, _ := json.Marshal(protocol.RenameWorkspaceResponse{
		Success: err == nil,
		Error:   errStr,
	})
	_ = writer.Write(protocol.Envelope{
		Type:    protocol.MsgRenameWorkspaceRes,
		Payload: resData,
	})
}

func (d *Daemon) handleStopServerMsg(writer *protocol.EnvelopeWriter) {
	_ = writer.Write(protocol.Envelope{
		Type:    protocol.MsgStopServerRes,
		Payload: []byte(`"ok"`),
	})
	go func() {
		time.Sleep(50 * time.Millisecond)
		d.Stop()
		os.Exit(0)
	}()
}
