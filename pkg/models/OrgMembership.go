package models

import "time"

// OrgMemberRole définit le rôle d'un membre dans une organisation
type OrgMemberRole string

const (
	OrgRoleOwner  OrgMemberRole = "owner"
	OrgRoleMember OrgMemberRole = "member"
)

// OrgMembership représente l'appartenance d'un utilisateur à une organisation
type OrgMembership struct {
	OrganizationID uint          `gorm:"primaryKey;not null;index"`
	UserID         uint          `gorm:"primaryKey;not null;index"`
	Role           OrgMemberRole `gorm:"type:varchar(20);default:'member';not null"`
	CreatedAt      time.Time

	// Relations
	Organization Organization `gorm:"foreignKey:OrganizationID"`
	User         User         `gorm:"foreignKey:UserID"`
}
