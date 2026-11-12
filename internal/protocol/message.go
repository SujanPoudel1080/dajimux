package protocol

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// MessageType indicates the type of IPC message payload.
type MessageType string

const (
	// Client -> Daemon
	MsgAttach        MessageType = "attach"
	MsgInput         MessageType = "input"
	MsgResize        MessageType = "resize"
	MsgAction        MessageType = "action"
	MsgDetach        MessageType = "detach"

	// Daemon -> Client
	MsgFrameUpdate   MessageType = "frame_update"
	MsgLayoutState   MessageType = "layout_state"
	MsgBell          MessageType = "bell"
	MsgError         MessageType = "error"

	// Agent Mesh Protocol
	MsgAgentStatus    MessageType = "agent_status"
	MsgAgentSend      MessageType = "agent_send"
	MsgAgentBroadcast MessageType = "agent_broadcast"
	MsgPaneCapture    MessageType = "pane_capture"
	MsgPaneCaptureRes MessageType = "pane_capture_res"

	// Session & Server Management
	MsgListSessions    MessageType = "list_sessions"
	MsgListSessionsRes MessageType = "list_sessions_res"
	MsgListPanes       MessageType = "list_panes"
	MsgListPanesRes    MessageType = "list_panes_res"
	MsgSpawnPane       MessageType = "spawn_pane"
	MsgSpawnPaneRes    MessageType = "spawn_pane_res"
	MsgClosePane       MessageType = "close_pane"
	MsgClosePaneRes    MessageType = "close_pane_res"
	MsgKillSession     MessageType = "kill_session"
	MsgKillSessionRes  MessageType = "kill_session_res"
	MsgRenameSession   MessageType = "rename_session"
	MsgRenameSessionRes MessageType = "rename_session_res"
	MsgRenameWorkspace MessageType = "rename_workspace"
	MsgRenameWorkspaceRes MessageType = "rename_workspace_res"
	MsgStopServer      MessageType = "stop_server"
	MsgStopServerRes   MessageType = "stop_server_res"
)

// Mode represents the current modal state.
type Mode string

const (
	ModeInsert         Mode = "INSERT"
	ModeNormal         Mode = "NORMAL"
	ModeResize         Mode = "RESIZE"
	ModeScrollback     Mode = "SCROLLBACK"
	ModeCommandPalette Mode = "COMMAND_PALETTE"
)

// Envelope wraps all messages exchanged across Unix domain sockets or network streams.
type Envelope struct {
	Type      MessageType     `json:"type"`
	SessionID string          `json:"session_id,omitempty"`
	PaneID    string          `json:"pane_id,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

// AttachPayload is sent when a client connects to a session.
type AttachPayload struct {
	SessionName string `json:"session_name"`
	Cols        int    `json:"cols"`
	Rows        int    `json:"rows"`
	Term        string `json:"term"`
}

// InputPayload carries raw keystrokes or mouse events from client to daemon.
type InputPayload struct {
	Data []byte `json:"data"`
}

// ResizePayload carries terminal dimension changes (SIGWINCH).
type ResizePayload struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

// ActionPayload represents window management actions (splits, navigation, workspace jump).
type ActionPayload struct {
	Action string            `json:"action"` // "split_h", "split_v", "close", "nav_left", etc.
	Params map[string]string `json:"params,omitempty"`
}

// PaneInfo contains visual layout metadata for rendering borders and headers.
type PaneInfo struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	X          int    `json:"x"`
	Y          int    `json:"y"`
	W          int    `json:"w"`
	H          int    `json:"h"`
	IsActive   bool   `json:"is_active"`
	AgentState string `json:"agent_state,omitempty"`
	AgentBadge string `json:"agent_badge,omitempty"`
}

// WorkspaceMeta represents basic metadata for an active workspace.
type WorkspaceMeta struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// LayoutStatePayload notifies the client of the current screen arrangement.
type LayoutStatePayload struct {
	SessionName     string          `json:"session_name"`
	ActiveWorkspace int             `json:"active_workspace"`
	TotalWorkspaces int             `json:"total_workspaces"`
	Workspaces      []WorkspaceMeta `json:"workspaces,omitempty"`
	LayoutType      string          `json:"layout_type,omitempty"`
	ActivePaneID    string          `json:"active_pane_id"`
	Mode            Mode            `json:"mode"`
	Panes           []PaneInfo      `json:"panes"`
	IsMonocle       bool            `json:"is_monocle"`
}

// FrameUpdatePayload transmits raw differential ANSI bytes to render on the client screen.
type FrameUpdatePayload struct {
	ANSI []byte `json:"ansi"`
}

// AgentStatusPayload updates an agent's status indicator.
type AgentStatusPayload struct {
	State       string  `json:"state"` // "idle", "thinking", "executing", "waiting"
	Badge       string  `json:"badge"` // e.g. "🟡 Running Pytest"
	Progress    float32 `json:"progress,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// AgentMessagePayload allows panes/agents to send messages to each other.
type AgentMessagePayload struct {
	FromPaneID string `json:"from_pane_id"`
	ToTarget   string `json:"to_target"` // PaneID or channel name
	Content    string `json:"content"`
	Timestamp  int64  `json:"timestamp"`
}

// PaneCaptureRequest asks the daemon for lines from another pane.
type PaneCaptureRequest struct {
	TargetPaneID string `json:"target_pane_id"`
	Lines        int    `json:"lines"`
}

// PaneCaptureResponse contains captured clean lines from a pane.
type PaneCaptureResponse struct {
	TargetPaneID string   `json:"target_pane_id"`
	Lines        []string `json:"lines"`
}

// SessionSummary represents a high-level overview of an active session.
type SessionSummary struct {
	Name            string `json:"name"`
	WorkspacesCount int    `json:"workspaces_count"`
	PanesCount      int    `json:"panes_count"`
	ActiveWorkspace int    `json:"active_workspace"`
	AttachedClients int    `json:"attached_clients"`
}

// ListSessionsResponse contains the list of all active sessions in the daemon.
type ListSessionsResponse struct {
	Sessions []SessionSummary `json:"sessions"`
}

// ListPanesRequest requests detailed layout and process state for panes.
type ListPanesRequest struct {
	SessionName string `json:"session_name,omitempty"`
}

// DetailedPaneInfo contains process, geometry, and agent telemetry for a pane.
type DetailedPaneInfo struct {
	SessionName string `json:"session_name"`
	WorkspaceID int    `json:"workspace_id"`
	PaneID      string `json:"pane_id"`
	Title       string `json:"title"`
	Cwd         string `json:"cwd"`
	IsActive    bool   `json:"is_active"`
	AgentState  string `json:"agent_state,omitempty"`
	AgentBadge  string `json:"agent_badge,omitempty"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
}

// ListPanesResponse returns detailed information for queried panes.
type ListPanesResponse struct {
	Panes []DetailedPaneInfo `json:"panes"`
}

// SpawnPaneRequest dynamically creates a new pane in a session.
type SpawnPaneRequest struct {
	SessionName string `json:"session_name"`
	Direction   string `json:"direction,omitempty"` // "horizontal" or "vertical"
	Cwd         string `json:"cwd,omitempty"`
	Command     string `json:"command,omitempty"`
}

// SpawnPaneResponse returns the result of spawning a pane.
type SpawnPaneResponse struct {
	Success bool   `json:"success"`
	PaneID  string `json:"pane_id,omitempty"`
	Error   string `json:"error,omitempty"`
}

// ClosePaneRequest requests closing a specific pane by ID.
type ClosePaneRequest struct {
	PaneID string `json:"pane_id"`
}

// ClosePaneResponse contains the result of closing a pane.
type ClosePaneResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// KillSessionRequest requests the daemon to terminate a specific session.
type KillSessionRequest struct {
	SessionName string `json:"session_name"`
}

// KillSessionResponse contains the result of terminating a session.
type KillSessionResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// RenameSessionRequest requests renaming an active session.
type RenameSessionRequest struct {
	OldName string `json:"old_name"`
	NewName string `json:"new_name"`
}

// RenameSessionResponse contains the result of renaming a session.
type RenameSessionResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// RenameWorkspaceRequest requests renaming a workspace in a session.
type RenameWorkspaceRequest struct {
	SessionName string `json:"session_name"`
	WorkspaceID int    `json:"workspace_id"`
	NewName     string `json:"new_name"`
}

// RenameWorkspaceResponse contains the result of renaming a workspace.
type RenameWorkspaceResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// EnvelopeWriter provides synchronized writes so multiple goroutines can
// safely send envelopes on the same connection without interleaving bytes.
type EnvelopeWriter struct {
	mu sync.Mutex
	w  io.Writer
}

// NewEnvelopeWriter creates a thread-safe envelope writer.
func NewEnvelopeWriter(w io.Writer) *EnvelopeWriter {
	return &EnvelopeWriter{w: w}
}

// Write serializes and sends one envelope atomically.
func (ew *EnvelopeWriter) Write(env Envelope) error {
	data, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}
	data = append(data, '\n')

	ew.mu.Lock()
	_, err = ew.w.Write(data)
	ew.mu.Unlock()
	return err
}

// WriteEnvelope writes a JSON-encoded envelope followed by a newline delimiter.
func WriteEnvelope(w io.Writer, env Envelope) error {
	data, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}

// EnvelopeReader wraps a persistent json.Decoder so buffered bytes are never lost
// between successive reads on the same connection.
type EnvelopeReader struct {
	dec *json.Decoder
}

// NewEnvelopeReader creates a reader that should be reused for the lifetime of a connection.
func NewEnvelopeReader(r io.Reader) *EnvelopeReader {
	return &EnvelopeReader{dec: json.NewDecoder(r)}
}

// Read decodes the next envelope from the stream.
func (er *EnvelopeReader) Read() (*Envelope, error) {
	var env Envelope
	if err := er.dec.Decode(&env); err != nil {
		return nil, err
	}
	return &env, nil
}

// ReadEnvelope reads a newline-delimited JSON envelope from the reader.
// WARNING: Creates a new decoder each call — only safe for single-shot reads
// (e.g. one-off CLI commands). For persistent connections, use EnvelopeReader.
func ReadEnvelope(r io.Reader) (*Envelope, error) {
	var env Envelope
	dec := json.NewDecoder(r)
	if err := dec.Decode(&env); err != nil {
		return nil, err
	}
	return &env, nil
}
