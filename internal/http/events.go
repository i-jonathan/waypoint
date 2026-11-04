// Package httpapi serves Waypoint's local web API. This file holds the event
// broker that pushes "something changed" notices to open browser tabs.
package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Event is one notice sent to every open tab. Type is "changed" (a board's
// data moved on disk) or "projects" (the project list changed).
type Event struct {
	Type    string `json:"type"`
	Project string `json:"project"`
}

// subscriberBuffer is how many events a subscriber may fall behind by before
// further events are dropped for it.
const subscriberBuffer = 16

// pingInterval is how often an idle stream gets a comment line, so proxies and
// the browser see the connection is alive. A variable only so a test can speed it up.
var pingInterval = 25 * time.Second

// Broker fans events out to subscribers.
type Broker struct {
	mu   sync.Mutex
	subs map[chan Event]struct{}
}

func NewBroker() *Broker {
	return &Broker{subs: map[chan Event]struct{}{}}
}

// Publish hands e to every subscriber. It never blocks: a subscriber whose
// buffer is full misses the event.
func (b *Broker) Publish(e Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

// Subscribe returns a channel of events and a cancel func that unsubscribes
// and closes the channel. cancel is safe to call more than once.
func (b *Broker) Subscribe() (ch <-chan Event, cancel func()) {
	c := make(chan Event, subscriberBuffer)
	b.mu.Lock()
	b.subs[c] = struct{}{}
	b.mu.Unlock()
	return c, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if _, ok := b.subs[c]; ok {
			delete(b.subs, c)
			close(c)
		}
	}
}

func (b *Broker) subscriberCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}

// ServeSSE streams events to one client until its request context ends.
func (b *Broker) ServeSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	ch, cancel := b.Subscribe()
	defer cancel()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ping := time.NewTicker(pingInterval)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case e := <-ch:
			data, err := json.Marshal(e)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, data); err != nil {
				return
			}
			flusher.Flush()
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
