// Package sse is the in-process registry of open Server-Sent Events streams: it hands status events for a
// submission to every stream watching that submission on this replica.
package sse

import (
	"sync"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/Yeswanth03/distributed-judge-engine/services/submission-service/internal/model"
)

// Message is what a stream receives: a status event, or a request to re-read the submission from the
// database because events may have been missed (the broker subscription was re-established).
type Message struct {
	Event  *model.StatusEvent
	Resync bool
}

// Subscriber is one open stream's mailbox.
type Subscriber struct {
	SubmissionID uuid.UUID
	C            <-chan Message
	c            chan Message
}

var (
	activeStreams = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "sse_active_streams", Help: "Open SSE streams on this replica.",
	})
	droppedMessages = promauto.NewCounter(prometheus.CounterOpts{
		Name: "sse_dropped_messages_total", Help: "Messages dropped because a stream's buffer was full.",
	})
)

// A submission produces at most a handful of events, so a stream's buffer only fills if it has stopped
// reading; dropping then never blocks the broker consumer, and the client's reconnect re-reads the state.
const bufferSize = 16

type Hub struct {
	mu     sync.RWMutex
	subs   map[uuid.UUID]map[*Subscriber]struct{}
	done   chan struct{}
	closed bool
}

func NewHub() *Hub {
	return &Hub{subs: map[uuid.UUID]map[*Subscriber]struct{}{}, done: make(chan struct{})}
}

// Subscribe registers a stream for a submission. Always pair it with Unsubscribe.
func (h *Hub) Subscribe(id uuid.UUID) *Subscriber {
	c := make(chan Message, bufferSize)
	s := &Subscriber{SubmissionID: id, C: c, c: c}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs[id] == nil {
		h.subs[id] = map[*Subscriber]struct{}{}
	}
	h.subs[id][s] = struct{}{}
	activeStreams.Inc()
	return s
}

// Unsubscribe removes the stream. Calling it twice is harmless.
func (h *Hub) Unsubscribe(s *Subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	set := h.subs[s.SubmissionID]
	if _, ok := set[s]; !ok {
		return
	}
	delete(set, s)
	if len(set) == 0 {
		delete(h.subs, s.SubmissionID)
	}
	activeStreams.Dec()
}

// Publish delivers an event to the streams watching its submission. It never blocks.
func (h *Hub) Publish(ev model.StatusEvent) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for s := range h.subs[ev.SubmissionID] {
		send(s, Message{Event: &ev})
	}
}

// Resync asks every open stream to re-read its submission from the database.
func (h *Hub) Resync() {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, set := range h.subs {
		for s := range set {
			send(s, Message{Resync: true})
		}
	}
}

func send(s *Subscriber, m Message) {
	select {
	case s.c <- m:
	default:
		droppedMessages.Inc()
	}
}

// Done is closed when the hub shuts down; streams should end so the server can stop (clients reconnect to
// another replica).
func (h *Hub) Done() <-chan struct{} { return h.done }

// Close ends all streams. Safe to call more than once.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.closed {
		h.closed = true
		close(h.done)
	}
}

// Count is the number of open streams.
func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	n := 0
	for _, set := range h.subs {
		n += len(set)
	}
	return n
}
