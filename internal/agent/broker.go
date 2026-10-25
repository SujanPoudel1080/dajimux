package agent

import (
	"sync"
	"time"

	"github.com/SujanPoudel1080/dajimux/internal/protocol"
)

// PaneStatus represents the current telemetry state of an agent in a pane.
type PaneStatus struct {
	PaneID      string            `json:"pane_id"`
	State       string            `json:"state"` // "idle", "thinking", "executing", "waiting"
	Badge       string            `json:"badge"` // e.g. "🟡 Running Pytest"
	Progress    float32           `json:"progress,omitempty"`
	LastUpdated time.Time         `json:"last_updated"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// Message represents an inter-pane message.
type Message struct {
	ID        string    `json:"id"`
	FromPane  string    `json:"from_pane"`
	Target    string    `json:"target"` // pane ID, "@all", or "#channel"
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// Broker coordinates agent telemetry and inter-pane messaging.
type Broker struct {
	mu           sync.RWMutex
	statuses     map[string]*PaneStatus
	mailboxes    map[string][]Message
	subscriptions map[string]map[string]chan Message // channel -> (paneID -> chan)
	onStatusChange func(status *PaneStatus)
}

// NewBroker initializes an in-memory agent messaging and telemetry broker.
func NewBroker(onStatusChange func(status *PaneStatus)) *Broker {
	return &Broker{
		statuses:      make(map[string]*PaneStatus),
		mailboxes:     make(map[string][]Message),
		subscriptions: make(map[string]map[string]chan Message),
		onStatusChange: onStatusChange,
	}
}

// UpdateStatus sets or updates an agent's status.
func (b *Broker) UpdateStatus(paneID string, payload protocol.AgentStatusPayload) *PaneStatus {
	b.mu.Lock()
	defer b.mu.Unlock()

	status, exists := b.statuses[paneID]
	if !exists {
		status = &PaneStatus{
			PaneID:   paneID,
			Metadata: make(map[string]string),
		}
		b.statuses[paneID] = status
	}

	status.State = payload.State
	status.Badge = payload.Badge
	status.Progress = payload.Progress
	status.LastUpdated = time.Now()
	for k, v := range payload.Metadata {
		status.Metadata[k] = v
	}

	if b.onStatusChange != nil {
		b.onStatusChange(status)
	}

	return status
}

// GetStatus retrieves the current telemetry for a specific pane.
func (b *Broker) GetStatus(paneID string) *PaneStatus {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.statuses[paneID]
}

// Send delivers a message to a specific target pane or broadcast channel.
func (b *Broker) Send(fromPane, target, content string) Message {
	b.mu.Lock()
	defer b.mu.Unlock()

	msg := Message{
		FromPane:  fromPane,
		Target:    target,
		Content:   content,
		Timestamp: time.Now(),
	}

	// Direct pane delivery
	b.mailboxes[target] = append(b.mailboxes[target], msg)

	// Channel broadcast if target starts with '#' or is '@all'
	if subMap, exists := b.subscriptions[target]; exists {
		for _, ch := range subMap {
			select {
			case ch <- msg:
			default:
				// non-blocking if receiver queue is full
			}
		}
	}

	return msg
}

// Receive retrieves pending unread messages for a pane.
func (b *Broker) Receive(paneID string) []Message {
	b.mu.Lock()
	defer b.mu.Unlock()

	msgs := b.mailboxes[paneID]
	delete(b.mailboxes, paneID)
	return msgs
}

// Subscribe subscribes a pane to a channel.
func (b *Broker) Subscribe(paneID, channel string) <-chan Message {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, exists := b.subscriptions[channel]; !exists {
		b.subscriptions[channel] = make(map[string]chan Message)
	}

	ch := make(chan Message, 32)
	b.subscriptions[channel][paneID] = ch
	return ch
}

// Unsubscribe removes a pane's channel subscription.
func (b *Broker) Unsubscribe(paneID, channel string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if subMap, exists := b.subscriptions[channel]; exists {
		if ch, ok := subMap[paneID]; ok {
			close(ch)
			delete(subMap, paneID)
		}
		if len(subMap) == 0 {
			delete(b.subscriptions, channel)
		}
	}
}

// RemovePane cleans up telemetry and mailboxes when a pane is closed.
func (b *Broker) RemovePane(paneID string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	delete(b.statuses, paneID)
	delete(b.mailboxes, paneID)
	for channel, subMap := range b.subscriptions {
		if ch, ok := subMap[paneID]; ok {
			close(ch)
			delete(subMap, paneID)
		}
		if len(subMap) == 0 {
			delete(b.subscriptions, channel)
		}
	}
}
