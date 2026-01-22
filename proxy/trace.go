package proxy

import (
	"sync"
	"time"
)

// TraceEvent captures a high-level step in the proxy pipeline for observability.
type TraceEvent struct {
	Time       time.Time `json:"time"`
	Stage      string    `json:"stage"`      // discovery, blocklist, translate, forward, response
	Server     string    `json:"server"`     // backend/server name when applicable
	Method     string    `json:"method"`     // MCP method or HTTP verb
	Transport  string    `json:"transport"`  // http, stdio, sse, docker, etc.
	Detail     string    `json:"detail"`     // freeform description
	Attachment string    `json:"attachment"` // optional extra info (e.g., URI)
}

// TraceRecorder stores a bounded set of recent trace events.
type TraceRecorder struct {
	limit int
	mu    sync.RWMutex
	buf   []TraceEvent
}

// NewTraceRecorder creates a trace recorder with a fixed buffer size.
func NewTraceRecorder(limit int) *TraceRecorder {
	if limit <= 0 {
		limit = 200
	}
	return &TraceRecorder{limit: limit}
}

// Add records a new trace event.
func (tr *TraceRecorder) Add(event TraceEvent) {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	event.Time = time.Now()
	tr.buf = append(tr.buf, event)
	if len(tr.buf) > tr.limit {
		tr.buf = tr.buf[len(tr.buf)-tr.limit:]
	}
}

// List returns a copy of the current trace buffer in chronological order.
func (tr *TraceRecorder) List() []TraceEvent {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	out := make([]TraceEvent, len(tr.buf))
	copy(out, tr.buf)
	return out
}
