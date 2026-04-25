package service

import (
	"context"
	"strconv"

	"github.com/Gyt-project/backend-api/internal/orm"
	"github.com/Gyt-project/backend-api/pkg/models"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

// BranchProtectionService manages branch protection rules.
type BranchProtectionService struct{}

func (s *BranchProtectionService) Create(ctx context.Context, callerID uint, owner, repo, pattern string, requirePR bool, requiredApprovals int, dismissStale, blockForcePush bool) (*models.BranchProtection, error) {
	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	rule := &models.BranchProtection{
		RepositoryID:        r.ID,
		Pattern:             pattern,
		RequirePullRequest:  requirePR,
		RequiredApprovals:   requiredApprovals,
		DismissStaleReviews: dismissStale,
		BlockForcePush:      blockForcePush,
	}
	if err := orm.DB.Create(rule).Error; err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create branch protection: %v", err)
	}
	return rule, nil
}

func (s *BranchProtectionService) Get(ctx context.Context, owner, repo, id string) (*models.BranchProtection, error) {
	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	uid, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid id")
	}
	var rule models.BranchProtection
	if err := orm.DB.Where("id = ? AND repository_id = ?", uid, r.ID).First(&rule).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, status.Error(codes.NotFound, "branch protection rule not found")
		}
		return nil, status.Errorf(codes.Internal, "db error: %v", err)
	}
	return &rule, nil
}

func (s *BranchProtectionService) List(ctx context.Context, owner, repo string) ([]models.BranchProtection, error) {
	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	var rules []models.BranchProtection
	if err := orm.DB.Where("repository_id = ?", r.ID).Find(&rules).Error; err != nil {
		return nil, status.Errorf(codes.Internal, "db error: %v", err)
	}
	return rules, nil
}

func (s *BranchProtectionService) Update(ctx context.Context, callerID uint, owner, repo, id string, pattern *string, requirePR *bool, requiredApprovals *int, dismissStale *bool, blockForcePush *bool) (*models.BranchProtection, error) {
	rule, err := s.Get(ctx, owner, repo, id)
	if err != nil {
		return nil, err
	}
	if pattern != nil {
		rule.Pattern = *pattern
	}
	if requirePR != nil {
		rule.RequirePullRequest = *requirePR
	}
	if requiredApprovals != nil {
		rule.RequiredApprovals = *requiredApprovals
	}
	if dismissStale != nil {
		rule.DismissStaleReviews = *dismissStale
	}
	if blockForcePush != nil {
		rule.BlockForcePush = *blockForcePush
	}
	if err := orm.DB.Save(rule).Error; err != nil {
		return nil, status.Errorf(codes.Internal, "failed to update branch protection: %v", err)
	}
	return rule, nil
}

func (s *BranchProtectionService) Delete(ctx context.Context, callerID uint, owner, repo, id string) error {
	rule, err := s.Get(ctx, owner, repo, id)
	if err != nil {
		return err
	}
	return orm.DB.Delete(rule).Error
}

// MatchingRule returns the first protection rule that matches the given branch name (exact match or glob).
func (s *BranchProtectionService) MatchingRule(repoID uint, branchName string) *models.BranchProtection {
	var rules []models.BranchProtection
	orm.DB.Where("repository_id = ?", repoID).Find(&rules)
	for i := range rules {
		if matchPattern(rules[i].Pattern, branchName) {
			return &rules[i]
		}
	}
	return nil
}

// matchPattern does simple glob matching: exact or prefix with *.
func matchPattern(pattern, name string) bool {
	if pattern == name {
		return true
	}
	n := len(pattern)
	if n > 0 && pattern[n-1] == '*' {
		return len(name) >= n-1 && name[:n-1] == pattern[:n-1]
	}
	return false
}
