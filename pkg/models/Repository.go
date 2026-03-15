package models

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// OwnerType indique si le propriétaire est un utilisateur ou une organisation
type OwnerType string

const (
	OwnerTypeUser OwnerType = "user"
	OwnerTypeOrg  OwnerType = "org"
)

// Repository représente un dépôt GYT.
// Le champ GitRepoName correspond au nom utilisé côté soft-serve.
// Format : "<gitOwnerUsername>/<repoName>"  (ex: "alice/monrepo")
type Repository struct {
	BaseModel
	UUID          uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();uniqueIndex"`
	Name          string    `gorm:"not null;index"`
	Description   string    `gorm:"default:''"`
	DefaultBranch string    `gorm:"default:'main'"`
	IsPrivate     bool      `gorm:"default:false"`
	IsFork        bool      `gorm:"default:false"`
	ForkedFromID  *uint     `gorm:"index"` // auto-référence nullable
	Stars         int       `gorm:"default:0"`
	Forks         int       `gorm:"default:0"`
	GitRepoName   string    `gorm:"not null;uniqueIndex"` // nom côté soft-serve: "<gitOwnerUsername>/<repoName>"
	// Polymorphique : OwnerID + OwnerType pointent vers User ou Organization
	OwnerID   uint           `gorm:"not null;index"`
	OwnerType OwnerType      `gorm:"type:varchar(10);not null;index"`
	DeletedAt gorm.DeletedAt `gorm:"index"`

	// Relations
	ForkedFrom    *Repository        `gorm:"foreignKey:ForkedFromID"`
	Collaborators []RepoCollaborator `gorm:"foreignKey:RepositoryID"`
}

// RepoCollaborator représente un collaborateur direct sur un dépôt
type RepoCollaborator struct {
	RepositoryID uint   `gorm:"primaryKey;not null;index"`
	UserID       uint   `gorm:"primaryKey;not null;index"`
	AccessLevel  string `gorm:"type:varchar(20);default:'read';not null"` // "read", "write", "admin"

	// Relations
	Repository Repository `gorm:"foreignKey:RepositoryID"`
	User       User       `gorm:"foreignKey:UserID"`
}
