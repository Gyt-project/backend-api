package models

// Label représente un label attaché à un dépôt, utilisable sur les Issues et PR.
type Label struct {
	BaseModel
	RepositoryID uint   `gorm:"not null;index"`
	Name         string `gorm:"not null"`
	Color        string `gorm:"not null;default:'0075ca'"` // hex sans '#'
	Description  string `gorm:"default:''"`

	Repository Repository `gorm:"foreignKey:RepositoryID"`
}

