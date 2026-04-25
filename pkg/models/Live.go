package models

import "time"

// LiveSession represents a collaborative "Live PR Review" lobby.
// One lobby maps to exactly one open PullRequest.
// The creator opens the session; other reviewers join via WebSocket.
type LiveSession struct {
	BaseModel
	// PRID is the numeric PK of the PullRequest in the main backend DB.
	// Owner, Repo, and PRNumber are stored here so the live service can
	// call the backend gRPC API without a second DB lookup.
	PRID       uint       `gorm:"not null;index"`
	Owner      string     `gorm:"type:varchar(255);not null"`
	Repo       string     `gorm:"type:varchar(255);not null"`
	PRNumber   int32      `gorm:"not null"`
	PRAuthorID uint       `gorm:"not null;default:0"`
	CreatorID  uint       `gorm:"not null"`
	Title      string     `gorm:"type:varchar(255);default:'Live Review'"`
	Closed     bool       `gorm:"default:false"`
	ClosedAt   *time.Time `gorm:"index"`

	Creator      User              `gorm:"foreignKey:CreatorID"`
	Participants []LiveParticipant `gorm:"foreignKey:SessionID"`
	ChatMessages []LiveChatMessage `gorm:"foreignKey:SessionID"`
}

// LiveParticipant tracks every user that has joined (or left) a LiveSession.
// LeftAt is nil while the user is connected; set when they disconnect.
type LiveParticipant struct {
	BaseModel
	SessionID uint `gorm:"not null;index"`
	UserID    uint `gorm:"not null;index"`
	LeftAt    *time.Time

	Session LiveSession `gorm:"foreignKey:SessionID"`
	User    User        `gorm:"foreignKey:UserID"`
}

// LiveChatMessage is a chat message sent inside a LiveSession lobby.
// Persisted until the PR is closed.
type LiveChatMessage struct {
	BaseModel
	SessionID uint   `gorm:"not null;index"`
	UserID    uint   `gorm:"not null;index"`
	ParentID  *uint  `gorm:"index;default:null"` // optional reply-to
	Body      string `gorm:"type:text;not null"`

	Session LiveSession      `gorm:"foreignKey:SessionID"`
	User    User             `gorm:"foreignKey:UserID"`
	Parent  *LiveChatMessage `gorm:"foreignKey:ParentID"`
}

// LiveReviewEvent records real-time review events inside a session.
// Ephemeral events (cursor) are not persisted; durable events (draft,
// verdict) are stored here until the PR is closed.
type LiveReviewEvent struct {
	BaseModel
	SessionID uint   `gorm:"not null;index"`
	UserID    uint   `gorm:"not null;index"`
	EventType string `gorm:"type:varchar(30);not null"` // "draft" | "verdict"
	File      string `gorm:"type:varchar(500);default:''"`
	Line      int    `gorm:"default:0"`
	Payload   string `gorm:"type:text;default:''"` // raw JSON of the original message

	Session LiveSession `gorm:"foreignKey:SessionID"`
	User    User        `gorm:"foreignKey:UserID"`
}
