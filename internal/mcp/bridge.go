package mcp

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"

	"github.com/SujanPoudel1080/dajimux/internal/daemon"
	"github.com/SujanPoudel1080/dajimux/internal/protocol"
)

// DaemonBridge defines operations the MCP server uses to communicate with the multiplexer daemon.
type DaemonBridge interface {
	ListPanes(sessionName string) ([]protocol.DetailedPaneInfo, error)
	ReadPane(paneID string, lines int) ([]string, error)
	SendCommand(paneID, command string, pressEnter bool) error
	SpawnPane(session, direction, cwd, command string) (string, error)
	SetAgentStatus(paneID, state, badge string) error
}

// SocketDaemonBridge connects to the dajimux daemon over a Unix domain socket.
type SocketDaemonBridge struct {
	SocketPath string
}

// NewSocketDaemonBridge creates a daemon bridge targeting the given socket (or default if empty).
func NewSocketDaemonBridge(socketPath string) *SocketDaemonBridge {
	if socketPath == "" {
		socketPath = daemon.DefaultSocketPath()
	}
	return &SocketDaemonBridge{SocketPath: socketPath}
}

func (b *SocketDaemonBridge) dial() (net.Conn, *protocol.EnvelopeReader, error) {
	conn, err := net.Dial("unix", b.SocketPath)
	if err != nil {
		return nil, nil, fmt.Errorf("connect to daemon at %s: %w", b.SocketPath, err)
	}
	reader := protocol.NewEnvelopeReader(conn)
	return conn, reader, nil
}

func (b *SocketDaemonBridge) ListPanes(sessionName string) ([]protocol.DetailedPaneInfo, error) {
	conn, reader, err := b.dial()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	reqData, _ := json.Marshal(protocol.ListPanesRequest{SessionName: sessionName})
	if err := protocol.WriteEnvelope(conn, protocol.Envelope{
		Type:    protocol.MsgListPanes,
		Payload: reqData,
	}); err != nil {
		return nil, err
	}

	for i := 0; i < 10; i++ {
		env, err := reader.Read()
		if err != nil {
			return nil, err
		}
		if env.Type == protocol.MsgListPanesRes {
			var res protocol.ListPanesResponse
			if err := json.Unmarshal(env.Payload, &res); err != nil {
				return nil, err
			}
			return res.Panes, nil
		}
	}

	return nil, fmt.Errorf("timeout waiting for list_panes_res")
}

func (b *SocketDaemonBridge) ReadPane(paneID string, lines int) ([]string, error) {
	if lines <= 0 {
		lines = 50
	}

	conn, reader, err := b.dial()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	reqData, _ := json.Marshal(protocol.PaneCaptureRequest{
		TargetPaneID: paneID,
		Lines:        lines,
	})
	if err := protocol.WriteEnvelope(conn, protocol.Envelope{
		Type:    protocol.MsgPaneCapture,
		Payload: reqData,
	}); err != nil {
		return nil, err
	}

	for i := 0; i < 10; i++ {
		env, err := reader.Read()
		if err != nil {
			return nil, err
		}
		if env.Type == protocol.MsgPaneCaptureRes {
			var res protocol.PaneCaptureResponse
			if err := json.Unmarshal(env.Payload, &res); err != nil {
				return nil, err
			}
			return res.Lines, nil
		}
	}

	return nil, fmt.Errorf("timeout waiting for pane_capture_res")
}

func (b *SocketDaemonBridge) SendCommand(paneID, command string, pressEnter bool) error {
	conn, _, err := b.dial()
	if err != nil {
		return err
	}
	defer conn.Close()

	bytesToSend := []byte(command)
	if pressEnter && !strings.HasSuffix(command, "\n") {
		bytesToSend = append(bytesToSend, '\n')
	}

	payload, _ := json.Marshal(protocol.InputPayload{Data: bytesToSend})
	return protocol.WriteEnvelope(conn, protocol.Envelope{
		Type:    protocol.MsgInput,
		PaneID:  paneID,
		Payload: payload,
	})
}

func (b *SocketDaemonBridge) SpawnPane(session, direction, cwd, command string) (string, error) {
	conn, reader, err := b.dial()
	if err != nil {
		return "", err
	}
	defer conn.Close()

	reqData, _ := json.Marshal(protocol.SpawnPaneRequest{
		SessionName: session,
		Direction:   direction,
		Cwd:         cwd,
		Command:     command,
	})
	if err := protocol.WriteEnvelope(conn, protocol.Envelope{
		Type:    protocol.MsgSpawnPane,
		Payload: reqData,
	}); err != nil {
		return "", err
	}

	for i := 0; i < 10; i++ {
		env, err := reader.Read()
		if err != nil {
			return "", err
		}
		if env.Type == protocol.MsgSpawnPaneRes {
			var res protocol.SpawnPaneResponse
			if err := json.Unmarshal(env.Payload, &res); err != nil {
				return "", err
			}
			if !res.Success {
				return "", fmt.Errorf("spawn pane failed: %s", res.Error)
			}
			return res.PaneID, nil
		}
	}

	return "", fmt.Errorf("timeout waiting for spawn_pane_res")
}

func (b *SocketDaemonBridge) SetAgentStatus(paneID, state, badge string) error {
	conn, _, err := b.dial()
	if err != nil {
		return err
	}
	defer conn.Close()

	payload, _ := json.Marshal(protocol.AgentStatusPayload{
		State: state,
		Badge: badge,
	})
	return protocol.WriteEnvelope(conn, protocol.Envelope{
		Type:    protocol.MsgAgentStatus,
		PaneID:  paneID,
		Payload: payload,
	})
}
