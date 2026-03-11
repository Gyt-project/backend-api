package models

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Organization représente une organisation GYT.
// Côté git server (soft-serve), une org est représentée par un faux utilisateur
// avec la convention GitUsername = "org-<name>"
type Organization struct {
	BaseModel
	UUID        uuid.UUID      `gorm:"type:uuid;default:gen_random_uuid();uniqueIndex"`
	Name        string         `gorm:"uniqueIndex;not null"` // nom unique de l'org
	DisplayName string         `gorm:"default:''"`
	Description string         `gorm:"default:''"`
	AvatarURL   string         `gorm:"default:''"`
	GitUsername string         `gorm:"uniqueIndex;not null"` // faux user soft-serve = "org-<name>"
	GitID       string         `gorm:"uniqueIndex;not null"` // ID du faux user côté soft-serve
	OwnerID     uint           `gorm:"not null;index"`       // FK vers User (créateur/owner)
	DeletedAt   gorm.DeletedAt `gorm:"index"`

	// Relations
	Owner        User            `gorm:"foreignKey:OwnerID"`
	Members      []OrgMembership `gorm:"foreignKey:OrganizationID"`
	Repositories []Repository    `gorm:"polymorphic:Owner;polymorphicValue:org"`
}
