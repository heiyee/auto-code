package runtime

import (
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	cliEventTypeOutput      = "output"
	cliEventTypeState       = "state"
	cliEventTypeSideError   = "side_error"
	cliEventTypeReplayReset = "replay_reset"
	cliEventBufferSize      = 2048
	cliEventReplayLimit     = 512
)

type CLIEvent struct {
	EventID   string            `json:"event_id"`
	Type      string            `json:"type"`
	SessionID string            `json:"session_id"`
	AgentID   string            `json:"agentid"`
	Seq       int64             `json:"seq"`
	Timestamp time.Time         `json:"timestamp"`
	RawB64    string            `json:"raw_b64,omitempty"`
	Output    string            `json:"output,omitempty"`
	SideError *RuntimeSideError `json:"side_error,omitempty"`
	State     string            `json:"state,omitempty"`
	Done      bool              `json:"done,omitempty"`
	ExitCode  *int              `json:"exit_code,omitempty"`
	LastError string            `json:"last_error,omitempty"`
}

type CLIEventFilter struct {
	SessionID string
	AgentID   string
}

// matches checks whether one event satisfies the subscription filter.
func (f CLIEventFilter) matches(event CLIEvent) bool {
	if f.SessionID != "" && event.SessionID != f.SessionID {
		return false
	}
	if f.AgentID != "" && event.AgentID != f.AgentID {
		return false
	}
	return true
}

type CLIEventSubscription struct {
	Events  <-chan CLIEvent
	closeFn func()
}

// Close unsubscribes the consumer and releases resources.
func (s *CLIEventSubscription) Close() {
	if s == nil || s.closeFn == nil {
		return
	}
	s.closeFn()
}

type cliEventSubscriber struct {
	id     uint64
	filter CLIEventFilter
	events chan CLIEvent
}

type CLIEventHub struct {
	mu            sync.RWMutex
	pipeline      *CLIOutputPipeline
	subscribers   map[uint64]*cliEventSubscriber
	sessionSeq    map[string]int64
	sessionReplay map[string][]CLIEvent
	nextEventID   uint64
	nextSubscript uint64
}

// NewCLIEventHub creates an event hub with an optional output processor pipeline.
func NewCLIEventHub(pipeline *CLIOutputPipeline) *CLIEventHub {
	return &CLIEventHub{
		pipeline:      pipeline,
		subscribers:   make(map[uint64]*cliEventSubscriber),
		sessionSeq:    make(map[string]int64),
		sessionReplay: make(map[string][]CLIEvent),
	}
}

// AddProcessor appends one output processor to the shared event pipeline.
func (h *CLIEventHub) AddProcessor(processor CLIOutputProcessor) {
	if h == nil {
		return
	}
	h.mu.Lock()
	if h.pipeline == nil {
		h.pipeline = NewCLIOutputPipeline()
	}
	pipeline := h.pipeline
	h.mu.Unlock()
	pipeline.Add(processor)
}

// ClearSession clears per-session state in processors and sequence trackers.
func (h *CLIEventHub) ClearSession(sessionID, agentID string) {
	if h == nil {
		return
	}
	h.mu.RLock()
	pipeline := h.pipeline
	h.mu.RUnlock()
	if pipeline != nil {
		pipeline.ClearSession(sessionID, agentID)
	}

	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	h.mu.Lock()
	delete(h.sessionSeq, sessionID)
	delete(h.sessionReplay, sessionID)
	h.mu.Unlock()
}

// CurrentSequence returns the latest emitted sequence number for one session.
func (h *CLIEventHub) CurrentSequence(sessionID string) int64 {
	if h == nil {
		return 0
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return 0
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.sessionSeq[sessionID]
}

// EmitOutputChunk transforms one runtime output chunk and publishes it as an event.
func (h *CLIEventHub) EmitOutputChunk(chunk CLIOutputChunk) {
	if h == nil || len(chunk.Payload) == 0 {
		return
	}
	if chunk.Timestamp.IsZero() {
		chunk.Timestamp = time.Now()
	}
	chunk.SessionID = strings.TrimSpace(chunk.SessionID)
	chunk.AgentID = strings.TrimSpace(chunk.AgentID)
	if chunk.SessionID == "" || chunk.AgentID == "" {
		return
	}

	eventTimestamp := chunk.Timestamp
	var processedPayload []byte
	h.mu.RLock()
	pipeline := h.pipeline
	h.mu.RUnlock()
	if pipeline != nil {
		processed := pipeline.Process(chunk)
		if !processed.Timestamp.IsZero() {
			eventTimestamp = processed.Timestamp
		}
		if len(processed.Payload) > 0 {
			processedPayload = processed.Payload
		}
	}

	event := CLIEvent{
		Type:      cliEventTypeOutput,
		SessionID: chunk.SessionID,
		AgentID:   chunk.AgentID,
		Timestamp: eventTimestamp,
		RawB64:    base64.StdEncoding.EncodeToString(chunk.Payload),
	}
	if len(processedPayload) > 0 {
		event.Output = string(processedPayload)
	}
	h.publish(event)
}

// EmitStateChange publishes one runtime state transition event.
func (h *CLIEventHub) EmitStateChange(summary SessionSummary) {
	if h == nil {
		return
	}
	summary.ID = strings.TrimSpace(summary.ID)
	summary.AgentID = strings.TrimSpace(summary.AgentID)
	if summary.ID == "" || summary.AgentID == "" {
		return
	}
	timestamp := summary.UpdatedAt
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	h.publish(CLIEvent{
		Type:      cliEventTypeState,
		SessionID: summary.ID,
		AgentID:   summary.AgentID,
		Timestamp: timestamp,
		State:     strings.TrimSpace(summary.State),
		Done:      strings.EqualFold(strings.TrimSpace(summary.State), "exited"),
		ExitCode:  summary.ExitCode,
		LastError: summary.LastError,
	})
}

// EmitSideError publishes one runtime side-channel error event.
func (h *CLIEventHub) EmitSideError(sessionID, agentID string, sideErr RuntimeSideError) {
	if h == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	agentID = strings.TrimSpace(agentID)
	if sessionID == "" || agentID == "" || strings.TrimSpace(sideErr.ID) == "" {
		return
	}
	if sideErr.Timestamp.IsZero() {
		sideErr.Timestamp = time.Now()
	}
	h.publish(CLIEvent{
		Type:      cliEventTypeSideError,
		SessionID: sessionID,
		AgentID:   agentID,
		Timestamp: sideErr.Timestamp,
		SideError: &sideErr,
	})
}

// publish assigns event metadata and fan-outs the event to subscribers.
func (h *CLIEventHub) publish(event CLIEvent) {
	h.mu.Lock()
	event.EventID = fmt.Sprintf("evt-%012d", atomic.AddUint64(&h.nextEventID, 1))
	nextSeq := h.sessionSeq[event.SessionID] + 1
	h.sessionSeq[event.SessionID] = nextSeq
	event.Seq = nextSeq
	h.appendReplayLocked(event)
	toClose := make([]uint64, 0)
	for _, sub := range h.subscribers {
		if !sub.filter.matches(event) {
			continue
		}
		select {
		case sub.events <- event:
		default:
			toClose = append(toClose, sub.id)
		}
	}
	for _, id := range toClose {
		sub, ok := h.subscribers[id]
		if !ok {
			continue
		}
		delete(h.subscribers, id)
		close(sub.events)
	}
	h.mu.Unlock()
}

func (h *CLIEventHub) appendReplayLocked(event CLIEvent) {
	if strings.TrimSpace(event.SessionID) == "" {
		return
	}
	buffer := append(h.sessionReplay[event.SessionID], event)
	if overflow := len(buffer) - cliEventReplayLimit; overflow > 0 {
		buffer = append([]CLIEvent(nil), buffer[overflow:]...)
	}
	h.sessionReplay[event.SessionID] = buffer
}

func (h *CLIEventHub) replayEventsLocked(filter CLIEventFilter, lastSeq int64) ([]CLIEvent, bool) {
	if lastSeq <= 0 || strings.TrimSpace(filter.SessionID) == "" {
		return nil, true
	}
	currentSeq := h.sessionSeq[filter.SessionID]
	if lastSeq >= currentSeq {
		return nil, true
	}
	buffer := h.sessionReplay[filter.SessionID]
	if len(buffer) == 0 {
		return nil, false
	}
	oldestSeq := buffer[0].Seq
	if lastSeq < oldestSeq-1 {
		return nil, false
	}
	replay := make([]CLIEvent, 0, len(buffer))
	for _, event := range buffer {
		if event.Seq <= lastSeq {
			continue
		}
		if !filter.matches(event) {
			continue
		}
		replay = append(replay, event)
	}
	return replay, true
}

// Subscribe registers one filtered subscriber and returns its event stream.
func (h *CLIEventHub) Subscribe(filter CLIEventFilter, lastSeq int64) *CLIEventSubscription {
	if h == nil {
		return &CLIEventSubscription{Events: make(chan CLIEvent)}
	}
	filter.SessionID = strings.TrimSpace(filter.SessionID)
	filter.AgentID = strings.TrimSpace(filter.AgentID)

	events := make(chan CLIEvent, cliEventBufferSize)
	subID := atomic.AddUint64(&h.nextSubscript, 1)
	sub := &cliEventSubscriber{
		id:     subID,
		filter: filter,
		events: events,
	}

	h.mu.Lock()
	replay, complete := h.replayEventsLocked(filter, lastSeq)
	h.subscribers[subID] = sub
	h.mu.Unlock()

	if !complete {
		events <- CLIEvent{
			Type:      cliEventTypeReplayReset,
			SessionID: filter.SessionID,
			AgentID:   filter.AgentID,
			Timestamp: time.Now(),
		}
	} else {
		for _, event := range replay {
			events <- event
		}
	}

	return &CLIEventSubscription{
		Events: events,
		closeFn: func() {
			h.removeSubscribers([]uint64{subID})
		},
	}
}

// removeSubscribers removes subscribers by id and closes their channels.
func (h *CLIEventHub) removeSubscribers(ids []uint64) {
	if h == nil || len(ids) == 0 {
		return
	}
	h.mu.Lock()
	for _, id := range ids {
		sub, ok := h.subscribers[id]
		if !ok {
			continue
		}
		delete(h.subscribers, id)
		close(sub.events)
	}
	h.mu.Unlock()
}
