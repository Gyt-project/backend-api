package models

import "time"

// PullRequest représente une demande de fusion entre deux branches.
type PullRequest struct {
	BaseModel
	RepositoryID uint   `gorm:"not null;index"`
	Number       int    `gorm:"not null;index"`
	Title        string `gorm:"not null"`
	Body         string `gorm:"type:text;default:''"`
	State        string `gorm:"type:varchar(10);default:'open';index"` // "open" | "closed" | "merged"
	HeadBranch   string `gorm:"not null"`
	BaseBranch   string `gorm:"not null"`
	HeadSHA      string `gorm:"not null"`
	AuthorID     uint   `gorm:"not null;index"`
	Mergeable    bool   `gorm:"default:true"`
	Merged       bool   `gorm:"default:false"`
	MergedAt     *time.Time

	Repository     Repository      `gorm:"foreignKey:RepositoryID"`
	Author         User            `gorm:"foreignKey:AuthorID"`
	Assignees      []User          `gorm:"many2many:pr_assignees;"`
	Labels         []Label         `gorm:"many2many:pr_labels;"`
	Comments       []PRComment     `gorm:"foreignKey:PullRequestID"`
	Reviews        []PRReview      `gorm:"foreignKey:PullRequestID"`
	ReviewRequests []ReviewRequest `gorm:"foreignKey:PullRequestID"`
}

// PRComment représente un commentaire (général ou inline) sur une PR.
type PRComment struct {
	BaseModel
	PullRequestID uint    `gorm:"not null;index"`
	AuthorID      uint    `gorm:"not null;index"`
	Body          string  `gorm:"type:text;not null"`
	Path          *string // chemin du fichier pour commentaire inline
	Line          *int    // numéro de ligne pour commentaire inline

	PullRequest PullRequest `gorm:"foreignKey:PullRequestID"`
	Author      User        `gorm:"foreignKey:AuthorID"`
}

// PRReview représente une revue formelle sur une PR.
type PRReview struct {
	BaseModel
	PullRequestID uint       `gorm:"not null;index"`
	ReviewerID    uint       `gorm:"not null;index"`
	State         string     `gorm:"type:varchar(20);not null"` // "APPROVED" | "CHANGES_REQUESTED" | "COMMENTED" | "DISMISSED"
	Body          string     `gorm:"type:text;default:''"`
	Dismissed     bool       `gorm:"default:false"`
	DismissedAt   *time.Time
	DismissReason string `gorm:"type:text;default:''"`

	PullRequest PullRequest `gorm:"foreignKey:PullRequestID"`
	Reviewer    User        `gorm:"foreignKey:ReviewerID"`
}

// ReviewRequest représente une demande de revue formelle adressée à un utilisateur.
type ReviewRequest struct {
	BaseModel
	PullRequestID uint `gorm:"not null;index"`
	ReviewerID    uint `gorm:"not null;index"`
	RequestedByID uint `gorm:"not null"`

	PullRequest PullRequest `gorm:"foreignKey:PullRequestID"`
	Reviewer    User        `gorm:"foreignKey:ReviewerID"`
	RequestedBy User        `gorm:"foreignKey:RequestedByID"`
}

