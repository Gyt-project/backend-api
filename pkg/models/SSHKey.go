package models

import "time"

// SSHKey représente une clé SSH publique associée à un utilisateur
type SSHKey struct {
	BaseModel
	UserID    uint   `gorm:"not null;index"`
	Name      string `gorm:"not null"`
	PublicKey string `gorm:"not null;uniqueIndex"`
	CreatedAt time.Time

	// Relations
	User User `gorm:"foreignKey:UserID"`
}
