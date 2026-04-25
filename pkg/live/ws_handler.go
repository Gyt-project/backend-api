package live

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/Gyt-project/backend-api/internal/auth"
	"github.com/Gyt-project/backend-api/internal/orm"
	"github.com/Gyt-project/backend-api/pkg/models"
	"github.com/gorilla/websocket"
)

const (
	// writeWait is the maximum time allowed to write a message to the client.
	writeWait = 10 * time.Second
	// pongWait is the time we wait for a pong response before assuming the
	// connection is dead.
	pongWait = 60 * time.Second
	// pingPeriod is how often the server sends a ping to keep the connection
	// alive.  Must be less than pongWait.
	pingPeriod = 50 * time.Second
	// maxMsgSize caps incoming WebSocket message sizes to prevent abuse.
	maxMsgSize = 64 * 1024 // 64 KB
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// CheckOrigin must be tightened in production to the actual frontend origin.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Client represents one active WebSocket connection inside a Hub.
type Client struct {
	hub       *Hub
	conn      *websocket.Conn
	send      chan []byte
	userID    uint
	username  string
	token     string // JWT access token (stored for server-side gRPC calls)
	sessionID uint
}

// WSHandler is the HTTP handler that upgrades connections to WebSocket and
// manages the full lifecycle of a Live PR participant.
//
// URL: GET /live/ws?session={sessionID}
//
// After the upgrade the client must send an AuthMessage as its very first
// message.  Any subsequent message that arrives before auth is accepted causes
// the connection to be closed.
func WSHandler(w http.ResponseWriter, r *http.Request) {
	// ── Parse session ID ──────────────────────────────────────────────────────
	sessionID64, err := strconv.ParseUint(r.URL.Query().Get("session"), 10, 64)
	if err != nil || sessionID64 == 0 {
		http.Error(w, "invalid session", http.StatusBadRequest)
		return
	}
	sessionID := uint(sessionID64)

	// ── Validate session ──────────────────────────────────────────────────────
	var session models.LiveSession
	if err := orm.DB.Where("id = ? AND closed = false", sessionID).First(&session).Error; err != nil {
		http.Error(w, "session not found or closed", http.StatusNotFound)
		return
	}

	// ── WebSocket upgrade ─────────────────────────────────────────────────────
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[ws] upgrade error: %v", err)
		return
	}

	c := &Client{
		conn:      conn,
		send:      make(chan []byte, 256),
		sessionID: sessionID,
	}

	// ── Auth handshake ────────────────────────────────────────────────────────
	// The first message must arrive within 15 seconds and must be an auth frame.
	if err := conn.SetReadDeadline(time.Now().Add(15 * time.Second)); err != nil {
		conn.Close()
		return
	}
	_, raw, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return
	}
	// Reset deadline for normal operation.
	_ = conn.SetReadDeadline(time.Time{})

	var authMsg AuthMessage
	if err := json.Unmarshal(raw, &authMsg); err != nil || authMsg.Type != EventAuth {
		wsClose(conn, "expected auth message")
		return
	}

	claims, err := auth.ParseAccessToken(authMsg.Token)
	if err != nil {
		wsClose(conn, "invalid token")
		return
	}

	// ── Load user info ────────────────────────────────────────────────────────
	var user models.User
	if err := orm.DB.Select("id, username, avatar_url").First(&user, claims.UserID).Error; err != nil {
		wsClose(conn, "user not found")
		return
	}

	c.userID = user.ID
	c.username = user.Username
	c.token = authMsg.Token

	// ── Register in Hub ───────────────────────────────────────────────────────
	hub := Manager.GetOrCreate(sessionID)
	c.hub = hub
	hub.register <- c

	// ── Persist join ─────────────────────────────────────────────────────────
	recordJoin(sessionID, user.ID)

	// ── Send auth_ok + presence ───────────────────────────────────────────────
	sendJSON(c.send, OutEvent{
		Type: EventAuthOK,
		Payload: AuthOKPayload{
			SessionID: sessionID,
			User:      UserInfo{ID: user.ID, Username: user.Username, Avatar: user.AvatarURL},
		},
	})
	presence := buildPresencePayload(sessionID, hub)
	sendJSON(c.send, OutEvent{Type: EventPresence, Payload: presence})

	// ── Replay chat history so late joiners can catch up ──────────────────────
	history := loadChatHistory(sessionID)
	sendJSON(c.send, OutEvent{
		Type:    EventChatHistory,
		Payload: ChatHistoryPayload{Messages: history},
	})

	// ── Notify other participants ─────────────────────────────────────────────
	hub.Publish(r.Context(), OutEvent{
		Type:    EventUserJoined,
		Payload: UserJoinedPayload{User: UserInfo{ID: user.ID, Username: user.Username, Avatar: user.AvatarURL}},
	})

	// ── Run write pump concurrently, read pump blocks this goroutine ──────────
	go c.writePump()
	c.readPump()

	// ── Cleanup after read pump returns (connection closed) ───────────────────
	// Remove this user from any active proposal before unregistering.
	hub.OnParticipantLeft(context.Background(), user.ID)
	hub.unregister <- c
	recordLeave(sessionID, user.ID)
	hub.Publish(context.Background(), OutEvent{
		Type:    EventUserLeft,
		Payload: UserLeftPayload{UserID: c.userID},
	})
}

// readPump runs in the calling goroutine and processes inbound messages until
// the connection closes or pong times out.
func (c *Client) readPump() {
	defer c.conn.Close()
	c.conn.SetReadLimit(maxMsgSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway,
				websocket.CloseNormalClosure,
				websocket.CloseNoStatusReceived,
			) {
				log.Printf("[ws] uid=%d unexpected close: %v", c.userID, err)
			}
			return
		}
		c.handleMessage(raw)
	}
}

// writePump runs in its own goroutine and serialises writes to the WebSocket.
// It also sends periodic pings to detect dead connections.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Hub closed the channel.
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleMessage dispatches an incoming client message by type.
func (c *Client) handleMessage(raw []byte) {
	var base struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &base); err != nil {
		return
	}

	ctx := context.Background()

	switch base.Type {
	case EventCursor:
		var m CursorMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			return
		}
		// Cursor events are ephemeral — broadcast only, not persisted.
		c.hub.Publish(ctx, OutEvent{
			Type:    EventCursor,
			Payload: CursorPayload{UserID: c.userID, File: m.File, Line: m.Line},
		})

	case EventDraft:
		var m DraftMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			return
		}
		persistReviewEvent(c.sessionID, c.userID, EventDraft, m.File, m.Line, string(raw))
		c.hub.Publish(ctx, OutEvent{
			Type:    EventDraft,
			Payload: DraftPayload{UserID: c.userID, File: m.File, Line: m.Line, Body: m.Body},
		})

	case EventChat:
		var m ChatMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			return
		}
		if m.Body == "" {
			return
		}
		msg := persistChatMessage(c.sessionID, c.userID, m.ParentID, m.Body)
		c.hub.Publish(ctx, OutEvent{
			Type: EventChat,
			Payload: ChatPayload{
				ID:        msg.ID,
				UserID:    c.userID,
				Username:  c.username,
				Body:      m.Body,
				ParentID:  m.ParentID,
				CreatedAt: msg.CreatedAt.UTC().Format(time.RFC3339),
			},
		})

	case EventVerdictPropose:
		var m VerdictProposeMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			return
		}
		// Load owner/repo/prNumber/prAuthorID so the hub can call the backend gRPC API on finalisation.
		var sess models.LiveSession
		if err := orm.DB.Select("owner, repo, pr_number, pr_author_id").First(&sess, c.sessionID).Error; err != nil {
			return
		}
		persistReviewEvent(c.sessionID, c.userID, EventVerdictPropose, "", 0, string(raw))
		if !c.hub.ProposeVerdict(ctx, c.userID, c.username, c.token, m.Decision, m.Body, sess.Owner, sess.Repo, sess.PRNumber, sess.PRAuthorID) {
			// Another proposal is already pending.
			sendJSON(c.send, OutEvent{
				Type:    "error",
				Payload: "a verdict proposal is already pending — wait for it to resolve",
			})
		}

	case EventVerdictAccept:
		var m VerdictVoteMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			return
		}
		c.hub.AcceptVerdict(ctx, c.userID, c.username, m.ProposalID)

	case EventVerdictReject:
		var m VerdictVoteMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			return
		}
		c.hub.RejectVerdict(ctx, c.userID, c.username, m.ProposalID)
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// sendJSON encodes v as JSON and enqueues it into the send channel.
func sendJSON(ch chan<- []byte, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	select {
	case ch <- data:
	default:
	}
}

// wsClose sends a JSON error message and then closes the connection.
func wsClose(conn *websocket.Conn, reason string) {
	data, _ := json.Marshal(OutEvent{Type: EventAuthFail, Payload: reason})
	_ = conn.WriteMessage(websocket.TextMessage, data)
	conn.Close()
}
