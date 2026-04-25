package live

import (
	"log"

	"github.com/Gyt-project/backend-api/internal/orm"
	"github.com/Gyt-project/backend-api/pkg/models"
)

// persistChatMessage writes a chat message to the DB and returns it
// (with the auto-assigned ID and timestamps populated).
func persistChatMessage(sessionID, userID uint, parentID *uint, body string) models.LiveChatMessage {
	msg := models.LiveChatMessage{
		SessionID: sessionID,
		UserID:    userID,
		ParentID:  parentID,
		Body:      body,
	}
	if err := orm.DB.Create(&msg).Error; err != nil {
		log.Printf("[live:chat] persist error: %v", err)
	}
	return msg
}

// persistReviewEvent records a durable review event (draft or verdict).
// Cursor events are intentionally excluded — they are ephemeral.
func persistReviewEvent(sessionID, userID uint, eventType, file string, line int, payload string) {
	e := models.LiveReviewEvent{
		SessionID: sessionID,
		UserID:    userID,
		EventType: eventType,
		File:      file,
		Line:      line,
		Payload:   payload,
	}
	if err := orm.DB.Create(&e).Error; err != nil {
		log.Printf("[live:event] persist error: %v", err)
	}
}
