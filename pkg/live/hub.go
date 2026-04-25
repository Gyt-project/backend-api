package live

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/Gyt-project/backend-api/internal/orm"
	"github.com/Gyt-project/backend-api/internal/pubsub"
	"github.com/Gyt-project/backend-api/pkg/models"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// verdictProposal tracks an in-flight collective verdict that all lobby
// participants must accept before the review is persisted.
type verdictProposal struct {
	id           string
	decision     string // "approve" | "request_changes" | "comment"
	body         string
	proposerID   uint
	proposerName string
	proposerToken string // JWT — used to authenticate the server-side gRPC call
	// owner / repo / prNumber identify the pull request in the backend so the
	// live service can call grpcClient.CreatePRReview without a direct DB query.
	owner    string
	repo     string
	prNumber int32
	votes    map[uint]bool // userID → true=accepted, false=pending
}

// Hub manages all active WebSocket connections for a single LiveSession.
//
// Architecture
//
//	┌─────────────┐       register/unregister       ┌──────────────┐
//	│  ws_handler │ ──────────────────────────────▶ │     Hub      │
//	└─────────────┘                                 │              │
//	                                                │  clients map │
//	Redis channel ──▶ readRedis() ──▶ fanOut() ──▶ │  send chans  │
//	                                                └──────────────┘
//
// When a client calls hub.Publish() the message is pushed to the Redis
// channel so every Hub instance (across multiple server replicas) that
// is subscribed to the same channel receives and fans it out locally.
// If Redis is unavailable the message is still delivered to local clients.
type Hub struct {
	sessionID uint
	channel   string // Redis Pub/Sub channel: "live:pr:{sessionID}:events"

	mu      sync.RWMutex
	clients map[uint]*Client // userID → active client

	broadcast  chan []byte  // local fan-out queue
	register   chan *Client // register a new client
	unregister chan *Client // unregister a disconnecting client

	// collective verdict proposal
	proposalMu sync.Mutex
	proposal   *verdictProposal

	redisSub *redis.PubSub

	ctx    context.Context
	cancel context.CancelFunc
}

// ─── HubManager ──────────────────────────────────────────────────────────────

// Manager is the global hub registry.  Access through its methods only.
var Manager = &HubManager{hubs: make(map[uint]*Hub)}

// HubManager creates and tracks one Hub per active LiveSession.
type HubManager struct {
	mu   sync.Mutex
	hubs map[uint]*Hub
}

// GetOrCreate returns the Hub for sessionID, creating and starting one if
// none exists.
func (m *HubManager) GetOrCreate(sessionID uint) *Hub {
	m.mu.Lock()
	defer m.mu.Unlock()
	if h, ok := m.hubs[sessionID]; ok {
		return h
	}
	h := newHub(sessionID)
	m.hubs[sessionID] = h
	go h.run()
	return h
}

// Remove stops and removes the Hub for sessionID.
func (m *HubManager) Remove(sessionID uint) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if h, ok := m.hubs[sessionID]; ok {
		h.cancel()
		delete(m.hubs, sessionID)
	}
}

// ─── Hub lifecycle ────────────────────────────────────────────────────────────

func newHub(sessionID uint) *Hub {
	ctx, cancel := context.WithCancel(context.Background())
	return &Hub{
		sessionID:  sessionID,
		channel:    fmt.Sprintf("live:pr:%d:events", sessionID),
		clients:    make(map[uint]*Client),
		broadcast:  make(chan []byte, 512),
		register:   make(chan *Client, 32),
		unregister: make(chan *Client, 32),
		ctx:        ctx,
		cancel:     cancel,
	}
}

func (h *Hub) run() {
	// Subscribe to Redis for cross-instance fan-out.
	sub, err := pubsub.Subscribe(h.ctx, h.channel)
	if err != nil {
		log.Printf("[hub:%d] redis subscribe failed (%v) — local fan-out only", h.sessionID, err)
	} else {
		h.redisSub = sub
		go h.readRedis()
	}

	for {
		select {
		case <-h.ctx.Done():
			if h.redisSub != nil {
				_ = h.redisSub.Close()
			}
			return

		case c := <-h.register:
			h.mu.Lock()
			h.clients[c.userID] = c
			h.mu.Unlock()

		case c := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[c.userID]; ok {
				delete(h.clients, c.userID)
				close(c.send)
			}
			h.mu.Unlock()

		case msg := <-h.broadcast:
			h.fanOut(msg)
		}
	}
}

// readRedis forwards every message received from the Redis channel into the
// local broadcast queue.
func (h *Hub) readRedis() {
	ch := h.redisSub.Channel()
	for {
		select {
		case <-h.ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			h.broadcast <- []byte(msg.Payload)
		}
	}
}

// fanOut writes msg to every locally connected client.
// Slow clients are skipped (drop policy) to avoid head-of-line blocking.
func (h *Hub) fanOut(msg []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, c := range h.clients {
		select {
		case c.send <- msg:
		default:
			log.Printf("[hub:%d] dropped message for slow client uid=%d", h.sessionID, c.userID)
		}
	}
}

// Publish serialises event to JSON, pushes it to the Redis channel (which
// triggers readRedis on every instance) and — if Redis is unavailable —
// falls back to direct local fan-out so the session still works.
func (h *Hub) Publish(ctx context.Context, event OutEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("[hub:%d] marshal error: %v", h.sessionID, err)
		return
	}
	if err := pubsub.Publish(ctx, h.channel, data); err != nil {
		// Redis unavailable — deliver locally only
		h.broadcast <- data
	}
	// When Redis is available, readRedis() picks up the published message
	// and queues it into broadcast, reaching all local clients including the
	// sender's own instance.
}

// Participants returns a snapshot of the IDs of all currently connected users.
func (h *Hub) Participants() []uint {
	h.mu.RLock()
	defer h.mu.RUnlock()
	ids := make([]uint, 0, len(h.clients))
	for id := range h.clients {
		ids = append(ids, id)
	}
	return ids
}

// ─── Collective verdict proposal ─────────────────────────────────────────────

// ProposeVerdict creates a new collective verdict proposal.
// The proposer automatically accepts.  The PR author is excluded from votes
// because they cannot review their own pull request.
// If a proposal is already active the call returns false and the existing
// proposal is unchanged.
func (h *Hub) ProposeVerdict(ctx context.Context, proposerID uint, proposerName, proposerToken, decision, body, owner, repo string, prNumber int32, prAuthorID uint) bool {
	h.proposalMu.Lock()
	defer h.proposalMu.Unlock()

	if h.proposal != nil {
		return false // already an active proposal
	}

	// Snapshot all currently connected participants, excluding the PR author.
	h.mu.RLock()
	votes := make(map[uint]bool, len(h.clients))
	for id := range h.clients {
		if id == prAuthorID {
			continue // PR author's vote is never required
		}
		votes[id] = false
	}
	h.mu.RUnlock()

	// Proposer auto-accepts.
	votes[proposerID] = true

	h.proposal = &verdictProposal{
		id:            uuid.New().String(),
		decision:      decision,
		body:          body,
		proposerID:    proposerID,
		proposerName:  proposerName,
		proposerToken: proposerToken,
		owner:         owner,
		repo:          repo,
		prNumber:      prNumber,
		votes:         votes,
	}

	total, accepted := h.tallyLocked()
	h.Publish(ctx, OutEvent{
		Type: EventVerdictProposed,
		Payload: VerdictProposedPayload{
			ProposalID:    h.proposal.id,
			ProposerID:    proposerID,
			ProposerName:  proposerName,
			Decision:      decision,
			Body:          body,
			TotalNeeded:   total,
			AcceptedCount: accepted,
		},
	})

	// If proposer is the only participant, finalise immediately.
	if accepted == total {
		h.finalizeLocked(ctx)
	}
	return true
}

// AcceptVerdict records an acceptance vote and finalises when unanimous.
func (h *Hub) AcceptVerdict(ctx context.Context, userID uint, username, proposalID string) {
	h.proposalMu.Lock()
	defer h.proposalMu.Unlock()

	if h.proposal == nil || h.proposal.id != proposalID {
		return // stale or no active proposal
	}

	h.proposal.votes[userID] = true
	total, accepted := h.tallyLocked()

	h.Publish(ctx, OutEvent{
		Type: EventVerdictVoteUpdate,
		Payload: VerdictVoteUpdatePayload{
			ProposalID:    proposalID,
			UserID:        userID,
			Username:      username,
			AcceptedCount: accepted,
			TotalNeeded:   total,
		},
	})

	if accepted == total {
		h.finalizeLocked(ctx)
	}
}

// RejectVerdict cancels the current proposal.
func (h *Hub) RejectVerdict(ctx context.Context, userID uint, username, proposalID string) {
	h.proposalMu.Lock()
	defer h.proposalMu.Unlock()

	if h.proposal == nil || h.proposal.id != proposalID {
		return
	}

	id := h.proposal.id
	h.proposal = nil

	h.Publish(ctx, OutEvent{
		Type: EventVerdictRejected,
		Payload: VerdictRejectedPayload{
			ProposalID: id,
			UserID:     userID,
			Username:   username,
		},
	})
}

// OnParticipantLeft removes a leaving user from the active proposal's vote
// map and finalises if all remaining participants have already accepted.
func (h *Hub) OnParticipantLeft(ctx context.Context, userID uint) {
	h.proposalMu.Lock()
	defer h.proposalMu.Unlock()

	if h.proposal == nil {
		return
	}

	delete(h.proposal.votes, userID)

	total, accepted := h.tallyLocked()
	if total == 0 {
		h.proposal = nil
		return
	}
	if accepted == total {
		h.finalizeLocked(ctx)
	}
}

// tallyLocked returns (total, accepted) for the current proposal.
// Must be called with proposalMu held.
func (h *Hub) tallyLocked() (total, accepted int) {
	for _, ok := range h.proposal.votes {
		total++
		if ok {
			accepted++
		}
	}
	return
}

// finalizeLocked persists the PRReview, creates draft comments, closes the
// session, and broadcasts verdict_finalized followed by session_closed.
// Must be called with proposalMu held.
func (h *Hub) finalizeLocked(ctx context.Context) {
	p := h.proposal
	h.proposal = nil

	reviewID := createCollectiveReview(p.owner, p.repo, p.prNumber, p.proposerID, p.proposerToken, p.decision, p.body)

	// Materialise all non-empty draft comments as real PR comments.
	createDraftComments(h.sessionID, p.owner, p.repo, p.prNumber, p.proposerToken)

	h.Publish(ctx, OutEvent{
		Type: EventVerdictFinalized,
		Payload: VerdictFinalizedPayload{
			ProposalID: p.id,
			Decision:   p.decision,
			Body:       p.body,
			ReviewID:   reviewID,
		},
	})

	// Mark the session as closed so new WebSocket connections are rejected.
	now := time.Now()
	if err := orm.DB.Model(&models.LiveSession{}).Where("id = ?", h.sessionID).Updates(map[string]interface{}{
		"closed":    true,
		"closed_at": now,
	}).Error; err != nil {
		log.Printf("[live:verdict] close session %d: %v", h.sessionID, err)
	}

	// Notify all connected clients that the lobby is closed.
	h.Publish(ctx, OutEvent{Type: EventSessionClosed})

	// Schedule hub removal after a short grace period so clients receive events.
	sessionID := h.sessionID
	go func() {
		time.Sleep(5 * time.Second)
		Manager.Remove(sessionID)
	}()
}
