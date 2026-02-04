package models

import "github.com/google/uuid"

type User struct {
	BaseModel
	UUID     uuid.UUID `gorm:"type:uuid;default:gen_random_uuid()"`
	Username string    `gorm:"unique;not null"`
	Email    string    `gorm:"unique;not null"`
	Password string    `gorm:"not null"`
	GitID    string    `gorm:"unique;not null"`
}
