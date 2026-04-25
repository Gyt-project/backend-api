package models

// BranchProtection defines protection rules for a branch pattern.
type BranchProtection struct {
	BaseModel
	RepositoryID        uint   `gorm:"not null;index"`
	Pattern             string `gorm:"not null"` // e.g. "main", "release/*"
	RequirePullRequest  bool   `gorm:"default:false"`
	RequiredApprovals   int    `gorm:"default:0"`
	DismissStaleReviews bool   `gorm:"default:false"`
	BlockForcePush      bool   `gorm:"default:false"`

	Repository Repository `gorm:"foreignKey:RepositoryID"`
}
