package models

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type User struct {
	BaseModel
	UUID        uuid.UUID      `gorm:"type:uuid;default:gen_random_uuid();uniqueIndex"`
	Username    string         `gorm:"uniqueIndex;not null"`
	Email       string         `gorm:"uniqueIndex;not null"`
	Password    string         `gorm:"not null"`
	DisplayName string         `gorm:"default:''"`
	Bio         string         `gorm:"default:''"`
	AvatarURL   string         `gorm:"default:''"`
	IsAdmin     bool           `gorm:"default:false"`
	GitUsername string         `gorm:"uniqueIndex;not null"` // username côté soft-serve (= Username)
	GitID       string         `gorm:"uniqueIndex;not null"` // ID interne soft-serve
	DeletedAt   gorm.DeletedAt `gorm:"index"`

	// Relations
	SSHKeys        []SSHKey        `gorm:"foreignKey:UserID"`
	Repositories   []Repository    `gorm:"polymorphic:Owner;polymorphicValue:user"`
	OrgMemberships []OrgMembership `gorm:"foreignKey:UserID"`
}
