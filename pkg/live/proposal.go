package live

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/Gyt-project/backend-api/internal/orm"
	pb "github.com/Gyt-project/backend-api/pkg/grpc"
	"github.com/Gyt-project/backend-api/pkg/models"
	"google.golang.org/grpc/metadata"
)

// createCollectiveReview calls the backend gRPC API to persist a PRReview
// on behalf of all lobby participants that unanimously accepted the verdict.
// The proposerToken (JWT) is injected as a Bearer Authorization header so the
// backend gRPC server can authenticate the caller.
// Returns the new review's ID (0 on error).
func createCollectiveReview(owner, repo string, prNumber int32, reviewerID uint, proposerToken, decision, body string) uint {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Inject the proposer's JWT so the backend gRPC interceptor can authenticate.
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(
		"authorization", "Bearer "+proposerToken,
	))

	resp, err := GrpcClient.CreatePRReview(ctx, &pb.CreatePRReviewRequest{
		Owner:  owner,
		Repo:   repo,
		Number: prNumber,
		State:  decisionToReviewState(decision),
		Body:   body,
	})
	if err != nil {
		log.Printf("[live:verdict] createCollectiveReview %s/%s#%d reviewerID=%d: %v",
			owner, repo, prNumber, reviewerID, err)
		return 0
	}
	return uint(resp.Id)
}

// createDraftComments materialises each participant's non-empty draft as a
// real PRComment on the pull request.  All comments are posted under the
// proposer's account (the only token we have).
func createDraftComments(sessionID uint, owner, repo string, prNumber int32, proposerToken string) {
	// Load all draft events for this session, oldest first.
	var events []models.LiveReviewEvent
	if err := orm.DB.
		Where("session_id = ? AND event_type = ?", sessionID, EventDraft).
		Order("id ASC").
		Find(&events).Error; err != nil {
		log.Printf("[live:verdict] createDraftComments load: %v", err)
		return
	}

	// Deduplicate: latest draft body per (userID, file, line).
	type draftKey struct {
		UserID uint
		File   string
		Line   int
	}
	latestBody := make(map[draftKey]string)
	for _, e := range events {
		var msg DraftMessage
		if err := json.Unmarshal([]byte(e.Payload), &msg); err != nil {
			continue
		}
		latestBody[draftKey{e.UserID, e.File, e.Line}] = msg.Body
	}

	if len(latestBody) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(
		"authorization", "Bearer "+proposerToken,
	))

	for key, body := range latestBody {
		if body == "" {
			continue // draft was cleared
		}
		req := &pb.CreatePRCommentRequest{
			Owner:  owner,
			Repo:   repo,
			Number: prNumber,
			Body:   body,
		}
		if key.File != "" {
			req.Path = &key.File
		}
		if key.Line > 0 {
			line := int32(key.Line)
			req.Line = &line
		}
		if _, err := GrpcClient.CreatePRComment(ctx, req); err != nil {
			log.Printf("[live:verdict] createDraftComments comment %s/%s#%d file=%q line=%d: %v",
				owner, repo, prNumber, key.File, key.Line, err)
		}
	}
}

// decisionToReviewState maps the Live PR decision string to the PRReview state
// enum expected by the backend (proto field: state).
func decisionToReviewState(decision string) string {
	switch decision {
	case "approve":
		return "APPROVE"
	case "request_changes":
		return "REQUEST_CHANGES"
	default:
		return "COMMENT"
	}
}
