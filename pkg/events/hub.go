// Package events provides an in-memory fan-out hub for real-time SSE events.
package events

import (
	"encoding/json"
	"sync"
)

// PREvent is a single event sent to SSE clients.
type PREvent struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// RepoEvent is a single event sent to repo-level SSE clients.
type RepoEvent struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type prHub struct {
	mu   sync.RWMutex
	subs map[string][]chan PREvent
}

type repoHub struct {
	mu   sync.RWMutex
	subs map[string][]chan RepoEvent
}

var (
	// PR is the global hub for PR-scoped events.
	// Key format: "owner/repo/number" (number is a decimal string).
	PR = &prHub{subs: make(map[string][]chan PREvent)}

	// Repo is the global hub for repo-scoped events.
	// Key format: "owner/repo"
	Repo = &repoHub{subs: make(map[string][]chan RepoEvent)}
)

// ── PR hub ───────────────────────────────────────────────────────────────────

func (h *prHub) Subscribe(key string) chan PREvent {
	ch := make(chan PREvent, 32)
	h.mu.Lock()
	h.subs[key] = append(h.subs[key], ch)
	h.mu.Unlock()
	return ch
}

func (h *prHub) Unsubscribe(key string, ch chan PREvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	subs := h.subs[key]
	for i, c := range subs {
		if c == ch {
			h.subs[key] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
	if len(h.subs[key]) == 0 {
		delete(h.subs, key)
	}
	close(ch)
}

func (h *prHub) Publish(key string, event PREvent) {
	h.mu.RLock()
	subs := make([]chan PREvent, len(h.subs[key]))
	copy(subs, h.subs[key])
	h.mu.RUnlock()
	for _, ch := range subs {
		select {
		case ch <- event:
		default: // drop if subscriber is slow
		}
	}
}

// PublishPR is a convenience helper.
func PublishPR(key string, eventType string) {
	PR.Publish(key, PREvent{Type: eventType})
}

// ── Repo hub ─────────────────────────────────────────────────────────────────

func (h *repoHub) Subscribe(key string) chan RepoEvent {
	ch := make(chan RepoEvent, 32)
	h.mu.Lock()
	h.subs[key] = append(h.subs[key], ch)
	h.mu.Unlock()
	return ch
}

func (h *repoHub) Unsubscribe(key string, ch chan RepoEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	subs := h.subs[key]
	for i, c := range subs {
		if c == ch {
			h.subs[key] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
	if len(h.subs[key]) == 0 {
		delete(h.subs, key)
	}
	close(ch)
}

func (h *repoHub) Publish(key string, event RepoEvent) {
	h.mu.RLock()
	subs := make([]chan RepoEvent, len(h.subs[key]))
	copy(subs, h.subs[key])
	h.mu.RUnlock()
	for _, ch := range subs {
		select {
		case ch <- event:
		default:
		}
	}
}

// PublishRepo is a convenience helper.
func PublishRepo(key string, eventType string) {
	Repo.Publish(key, RepoEvent{Type: eventType})
}
