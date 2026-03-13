package models

// Webhook représente un webhook configuré au niveau d'un dépôt ou d'une organisation.
type Webhook struct {
	BaseModel
	// Propriétaire : soit un repo, soit une org (polymorphique)
	OwnerID   uint      `gorm:"not null;index"`
	OwnerType OwnerType `gorm:"type:varchar(10);not null;index"` // "user"(repo owner) | "org"
	// Si nil → webhook d'org, sinon → webhook de repo
	RepositoryID *uint  `gorm:"index"`
	URL          string `gorm:"not null"`
	Secret       string `gorm:"default:''"`
	ContentType  string `gorm:"type:varchar(10);default:'json'"` // "json" | "form"
	Active       bool   `gorm:"default:true"`
	Events       string `gorm:"type:text;default:'[]'"` // JSON array de strings

	Repository *Repository `gorm:"foreignKey:RepositoryID"`
}

