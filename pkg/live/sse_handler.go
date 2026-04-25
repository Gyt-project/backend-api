package live

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/Gyt-project/backend-api/internal/auth"
	"github.com/Gyt-project/backend-api/internal/orm"
	"github.com/Gyt-project/backend-api/internal/pubsub"
	"github.com/Gyt-project/backend-api/pkg/models"
)

// SSEHandler streams server-sent events for a live session in read-only mode.
// Useful for clients that cannot use WebSocket (proxies, curl, etc.) or for
// embedding a live comment feed in other pages.
//
// URL: GET /live/events?session={sessionID}&token={jwtToken}
//
// Auth is passed as a query parameter because EventSource in browsers does
// not support custom request headers.
func SSEHandler(w http.ResponseWriter, r *http.Request) {
	// ── Auth ──────────────────────────────────────────────────────────────────
	tokenStr := r.URL.Query().Get("token")
	if _, err := auth.ParseAccessToken(tokenStr); err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// ── Session ───────────────────────────────────────────────────────────────
	sessionID64, err := strconv.ParseUint(r.URL.Query().Get("session"), 10, 64)
	if err != nil || sessionID64 == 0 {
		http.Error(w, "invalid session", http.StatusBadRequest)
		return
	}
	sessionID := uint(sessionID64)

	var session models.LiveSession
	if err := orm.DB.Where("id = ? AND closed = false", sessionID).First(&session).Error; err != nil {
		http.Error(w, "session not found or closed", http.StatusNotFound)
		return
	}

	// ── SSE headers ───────────────────────────────────────────────────────────
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported by this server", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Disable nginx / proxy buffering.
	w.Header().Set("X-Accel-Buffering", "no")

	// ── Subscribe to Redis channel ────────────────────────────────────────────
	channel := fmt.Sprintf("live:pr:%d:events", sessionID)
	sub, subErr := pubsub.Subscribe(r.Context(), channel)

	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	if subErr != nil {
		// Redis unavailable — send a comment every 30 s so the connection stays
		// alive, but no real events.
		for {
			select {
			case <-r.Context().Done():
				return
			case <-heartbeat.C:
				fmt.Fprintf(w, ": heartbeat\n\n")
				flusher.Flush()
			}
		}
	}

	defer sub.Close()
	redisCh := sub.Channel()

	for {
		select {
		case <-r.Context().Done():
			return

		case <-heartbeat.C:
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()

		case msg, ok := <-redisCh:
			if !ok {
				return
			}
			// Decode to filter out ephemeral cursor events.
			var evt OutEvent
			if err := json.Unmarshal([]byte(msg.Payload), &evt); err != nil {
				continue
			}
			if evt.Type == EventCursor {
				continue // cursor positions are not forwarded over SSE
			}
			fmt.Fprintf(w, "event: %s\n", evt.Type)
			fmt.Fprintf(w, "data: %s\n\n", msg.Payload)
			flusher.Flush()
		}
	}
}
