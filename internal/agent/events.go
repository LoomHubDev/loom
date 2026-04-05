package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// Event represents a server-sent event.
type Event struct {
	Type string      `json:"type"` // "checkpoint", "operation", "restore"
	Data interface{} `json:"data"`
}

// EventBus manages SSE subscribers.
type EventBus struct {
	mu          sync.RWMutex
	subscribers map[chan Event]struct{}
}

func NewEventBus() *EventBus {
	return &EventBus{subscribers: make(map[chan Event]struct{})}
}

// Subscribe returns a channel that receives events. Call Unsubscribe when done.
func (b *EventBus) Subscribe() chan Event {
	ch := make(chan Event, 32)
	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber channel.
func (b *EventBus) Unsubscribe(ch chan Event) {
	b.mu.Lock()
	delete(b.subscribers, ch)
	b.mu.Unlock()
	close(ch)
}

// Publish sends an event to all subscribers.
func (b *EventBus) Publish(evt Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subscribers {
		select {
		case ch <- evt:
		default:
			// Drop if subscriber is slow
		}
	}
}

func handleSSE(events *EventBus) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		ch := events.Subscribe()
		defer events.Unsubscribe(ch)

		for {
			select {
			case evt, ok := <-ch:
				if !ok {
					return
				}
				data, _ := json.Marshal(evt)
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, data)
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	}
}
