package models

import "time"

// Issue représente une issue ouverte sur un dépôt.
type Issue struct {
	BaseModel
	RepositoryID uint   `gorm:"not null;index"`
	Number       int    `gorm:"not null;index"`
	Title        string `gorm:"not null"`
	Body         string `gorm:"type:text;default:''"`
	State        string `gorm:"type:varchar(10);default:'open';index"` // "open" | "closed"
	AuthorID     uint   `gorm:"not null;index"`
	ClosedAt     *time.Time

	Repository Repository     `gorm:"foreignKey:RepositoryID"`
	Author     User           `gorm:"foreignKey:AuthorID"`
	Assignees  []User         `gorm:"many2many:issue_assignees;"`
	Labels     []Label        `gorm:"many2many:issue_labels;"`
	Comments   []IssueComment `gorm:"foreignKey:IssueID"`
}

// IssueComment représente un commentaire sur une issue.
type IssueComment struct {
	BaseModel
	IssueID  uint   `gorm:"not null;index"`
	AuthorID uint   `gorm:"not null;index"`
	Body     string `gorm:"type:text;not null"`

	Issue  Issue `gorm:"foreignKey:IssueID"`
	Author User  `gorm:"foreignKey:AuthorID"`
}

