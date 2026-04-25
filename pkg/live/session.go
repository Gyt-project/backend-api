package live

import (
	"log"
	"time"

	"github.com/Gyt-project/backend-api/internal/orm"
	"github.com/Gyt-project/backend-api/pkg/models"
	"gorm.io/gorm"
)

// recordJoin upserts a LiveParticipant row for the given user in the session.
// If the user has joined before (re-join after disconnect), LeftAt is cleared.
func recordJoin(sessionID, userID uint) {
	var p models.LiveParticipant
	err := orm.DB.
		Where("session_id = ? AND user_id = ?", sessionID, userID).
		First(&p).Error

	if err != nil {
		// First time joining this session.
		if dbErr := orm.DB.Create(&models.LiveParticipant{
			SessionID: sessionID,
			UserID:    userID,
		}).Error; dbErr != nil {
			log.Printf("[live:session] recordJoin create: %v", dbErr)
		}
		return
	}
	// Re-joining — clear the LeftAt timestamp.
	if dbErr := orm.DB.Model(&p).Update("left_at", nil).Error; dbErr != nil {
		log.Printf("[live:session] recordJoin update: %v", dbErr)
	}
}

// recordLeave marks the participant as having left the session.
func recordLeave(sessionID, userID uint) {
	now := time.Now()
	if err := orm.DB.Model(&models.LiveParticipant{}).
		Where("session_id = ? AND user_id = ?", sessionID, userID).
		Update("left_at", now).Error; err != nil {
		log.Printf("[live:session] recordLeave: %v", err)
	}
}

// loadChatHistory returns all chat messages for a session ordered by creation
// time, enriched with usernames.  The result is used to replay history to a
// newly (re-)connected participant.
func loadChatHistory(sessionID uint) []ChatPayload {
	var msgs []models.LiveChatMessage
	if err := orm.DB.
		Preload("User", func(db *gorm.DB) *gorm.DB {
			return db.Select("id, username")
		}).
		Where("session_id = ?", sessionID).
		Order("created_at ASC").
		Find(&msgs).Error; err != nil {
		log.Printf("[live:session] loadChatHistory: %v", err)
		return nil
	}
	out := make([]ChatPayload, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, ChatPayload{
			ID:        m.ID,
			UserID:    m.UserID,
			Username:  m.User.Username,
			Body:      m.Body,
			ParentID:  m.ParentID,
			CreatedAt: m.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	return out
}

// by cross-referencing the Hub's in-memory connection map with the DB.
func buildPresencePayload(sessionID uint, hub *Hub) PresencePayload {
	activeIDs := hub.Participants()
	users := make([]UserInfo, 0, len(activeIDs))

	if len(activeIDs) > 0 {
		var dbUsers []models.User
		if err := orm.DB.
			Where("id IN ?", activeIDs).
			Select("id, username, avatar_url").
			Find(&dbUsers).Error; err != nil {
			log.Printf("[live:session] buildPresencePayload: %v", err)
		}
		for _, u := range dbUsers {
			users = append(users, UserInfo{
				ID:       u.ID,
				Username: u.Username,
				Avatar:   u.AvatarURL,
			})
		}
	}

	return PresencePayload{SessionID: sessionID, Users: users}
}
