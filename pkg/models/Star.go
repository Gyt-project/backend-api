package models

// Star représente une étoile donnée par un utilisateur à un dépôt.
type Star struct {
	UserID       uint `gorm:"primaryKey;not null;index"`
	RepositoryID uint `gorm:"primaryKey;not null;index"`

	User       User       `gorm:"foreignKey:UserID"`
	Repository Repository `gorm:"foreignKey:RepositoryID"`
}

