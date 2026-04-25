package live

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/Gyt-project/backend-api/internal/auth"
	"github.com/Gyt-project/backend-api/internal/orm"
	pb "github.com/Gyt-project/backend-api/pkg/grpc"
	"github.com/Gyt-project/backend-api/pkg/models"
)

// ─── Session management REST handlers ────────────────────────────────────────

// CreateSessionHandler creates a new Live PR Review lobby.
//
//	POST /live/sessions
//	Authorization: Bearer <token>
//	Body: { "owner": "alice", "repo": "my-repo", "number": 42, "title": "optional" }
func CreateSessionHandler(w http.ResponseWriter, r *http.Request) {
	claims := requireAuth(w, r)
	if claims == nil {
		return
	}

	var req struct {
		Owner  string `json:"owner"`
		Repo   string `json:"repo"`
		Number int32  `json:"number"`
		Title  string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		req.Owner == "" || req.Repo == "" || req.Number == 0 {
		http.Error(w, "invalid body: owner, repo and number are required", http.StatusBadRequest)
		return
	}

	// Validate the PR exists and is open via the backend gRPC API.
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	prResp, err := GrpcClient.GetPullRequest(ctx, &pb.GetPRRequest{
		Owner:  req.Owner,
		Repo:   req.Repo,
		Number: req.Number,
	})
	if err != nil {
		http.Error(w, "PR not found", http.StatusNotFound)
		return
	}
	if prResp.State != "open" {
		http.Error(w, "PR is not open", http.StatusUnprocessableEntity)
		return
	}

	title := req.Title
	if title == "" {
		title = "Live Review"
	}

	// Look up the PR author's numeric DB ID so we can exclude them from
	// collective verdict votes (you cannot review your own PR).
	var prAuthorID uint
	if prResp.Author != nil && prResp.Author.Uuid != "" {
		var authorUser models.User
		if err := orm.DB.Select("id").Where("uuid = ?", prResp.Author.Uuid).First(&authorUser).Error; err == nil {
			prAuthorID = authorUser.ID
		}
	}

	session := models.LiveSession{
		PRID:       uint(prResp.Id),
		Owner:      req.Owner,
		Repo:       req.Repo,
		PRNumber:   req.Number,
		PRAuthorID: prAuthorID,
		CreatorID:  claims.UserID,
		Title:      title,
	}
	if err := orm.DB.Create(&session).Error; err != nil {
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(session)
}

// GetSessionHandler returns session metadata, all participants and the full
// chat history.
//
//	GET /live/sessions/{id}
//	Authorization: Bearer <token>
func GetSessionHandler(w http.ResponseWriter, r *http.Request) {
	if requireAuth(w, r) == nil {
		return
	}

	id64, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var session models.LiveSession
	if err := orm.DB.
		Preload("Participants.User").
		Preload("ChatMessages.User").
		First(&session, id64).Error; err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(session)
}

// ListSessionsForPRHandler lists all open Live Review sessions for a PR.
//
//	GET /live/pr/{prId}/sessions
//	Authorization: Bearer <token>
func ListSessionsForPRHandler(w http.ResponseWriter, r *http.Request) {
	if requireAuth(w, r) == nil {
		return
	}

	prID64, err := strconv.ParseUint(r.PathValue("prId"), 10, 64)
	if err != nil {
		http.Error(w, "invalid prId", http.StatusBadRequest)
		return
	}

	var sessions []models.LiveSession
	if err := orm.DB.
		Where("pr_id = ? AND closed = false", prID64).
		Order("created_at DESC").
		Find(&sessions).Error; err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sessions)
}

// CloseSessionHandler closes a live session.  Only the session creator may
// close it.
//
//	POST /live/sessions/{id}/close
//	Authorization: Bearer <token>
func CloseSessionHandler(w http.ResponseWriter, r *http.Request) {
	claims := requireAuth(w, r)
	if claims == nil {
		return
	}

	id64, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	sessionID := uint(id64)

	var session models.LiveSession
	if err := orm.DB.First(&session, sessionID).Error; err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	if session.CreatorID != claims.UserID {
		http.Error(w, "only the creator can close this session", http.StatusForbidden)
		return
	}

	now := time.Now()
	orm.DB.Model(&session).Updates(map[string]interface{}{
		"closed":    true,
		"closed_at": now,
	})

	// Notify all connected WebSocket clients.
	hub := Manager.GetOrCreate(sessionID)
	hub.Publish(context.Background(), OutEvent{Type: EventSessionClosed})
	Manager.Remove(sessionID)

	w.WriteHeader(http.StatusNoContent)
}

// ─── Auth helper ─────────────────────────────────────────────────────────────

// requireAuth extracts and validates the JWT from the Authorization header.
// On failure it writes the appropriate HTTP error and returns nil so the
// caller can return immediately.
func requireAuth(w http.ResponseWriter, r *http.Request) *auth.Claims {
	header := r.Header.Get("Authorization")
	token := ""
	if len(header) > 7 && header[:7] == "Bearer " {
		token = header[7:]
	}
	if token == "" {
		http.Error(w, "missing Authorization header", http.StatusUnauthorized)
		return nil
	}
	claims, err := auth.ParseAccessToken(token)
	if err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return nil
	}
	return claims
}
