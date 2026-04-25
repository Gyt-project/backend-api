package service

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/Gyt-project/backend-api/internal/cache"
	"github.com/Gyt-project/backend-api/internal/gitClient"
	"github.com/Gyt-project/backend-api/internal/orm"
	"github.com/Gyt-project/backend-api/pkg/models"
	ssgrpc "github.com/Gyt-project/soft-serve/pkg/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

// PRService gère la logique métier des Pull Requests.
type PRService struct{}

func (s *PRService) nextPRNumber(ctx context.Context, tx *gorm.DB, repoID uint) (int, error) {
	var max int

	err := tx.WithContext(ctx).
		Model(&models.PullRequest{}).
		Where("repository_id = ?", repoID).
		Select("COALESCE(MAX(number), 0)").
		Scan(&max).Error

	if err != nil {
		return 0, err
	}

	return max + 1, nil
}

func (s *PRService) loadPRBase(ctx context.Context, repoID uint, number int) (*models.PullRequest, error) {
	var pr models.PullRequest

	err := orm.DB.WithContext(ctx).
		Where("repository_id = ? AND number = ?", repoID, number).
		First(&pr).Error

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, status.Errorf(codes.NotFound, "pull request #%d not found", number)
		}
		return nil, status.Errorf(codes.Internal, "db error: %v", err)
	}

	return &pr, nil
}

func (s *PRService) loadPRFull(ctx context.Context, repoID uint, number int) (*models.PullRequest, error) {
	var pr models.PullRequest

	err := orm.DB.WithContext(ctx).
		Where("repository_id = ? AND number = ?", repoID, number).
		Preload("Author").
		Preload("Assignees").
		Preload("Labels").
		Preload("Comments.Author").
		Preload("Reviews.Reviewer").
		Preload("ReviewRequests.Reviewer").
		Preload("ReviewRequests.RequestedBy").
		First(&pr).Error

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, status.Errorf(codes.NotFound, "pull request #%d not found", number)
		}
		return nil, status.Errorf(codes.Internal, "db error: %v", err)
	}

	return &pr, nil
}

func (s *PRService) CreatePullRequest(ctx context.Context, callerID uint, owner, repo, title, head, base string, body *string, assignees, labels []string) (*models.PullRequest, error) {
	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	b := ""
	if body != nil {
		b = *body
	}
	var pr *models.PullRequest

	err = orm.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		nextNumber, err := s.nextPRNumber(ctx, tx, r.ID)
		if err != nil {
			return err
		}

		pr = &models.PullRequest{
			RepositoryID: r.ID,
			Number:       nextNumber,
			Title:        title,
			Body:         b,
			State:        "open",
			HeadBranch:   head,
			BaseBranch:   base,
			HeadSHA:      "",
			AuthorID:     callerID,
			Mergeable:    true,
		}

		if err := tx.Create(pr).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create pull request: %v", err)
	}
	s.attachPRAssignees(pr.ID, assignees)
	s.attachPRLabels(pr.ID, r.ID, labels)
	loaded, err := s.loadPRFull(ctx, r.ID, pr.Number)
	if err != nil {
		return nil, err
	}
	DispatchWebhook(r.ID, "pull_request", map[string]interface{}{
		"action":     "opened",
		"number":     loaded.Number,
		"title":      loaded.Title,
		"state":      loaded.State,
		"head":       head,
		"base":       base,
		"repository": owner + "/" + repo,
	})
	return loaded, nil
}

func (s *PRService) GetPullRequestBase(ctx context.Context, owner, repo string, number int) (*models.Repository, *models.PullRequest, error) {
	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return nil, nil, err
	}

	pr, err := s.loadPRBase(ctx, r.ID, number)
	if err != nil {
		return nil, nil, err
	}

	return r, pr, nil
}

func (s *PRService) GetPullRequestFull(ctx context.Context, owner, repo string, number int) (*models.PullRequest, error) {
	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	return s.loadPRFull(ctx, r.ID, number)
}

func (s *PRService) ListPullRequests(ctx context.Context, owner string, repo *string, state, author, assignee, label, base *string, page, perPage int) ([]models.PullRequest, int64, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 30
	}

	q := orm.DB.WithContext(ctx).Model(&models.PullRequest{}).
		Preload("Author").Preload("Assignees").Preload("Labels")

	if repo != nil && *repo != "" {
		r, err := resolveRepo(ctx, owner, *repo)
		if err != nil {
			return nil, 0, err
		}
		q = q.Where("repository_id = ?", r.ID)
	} else {
		rs := &RepoService{}
		ownerID, ownerType, _, err := rs.resolveOwner(ctx, owner)
		if err != nil {
			return nil, 0, err
		}
		var repoIDs []uint
		if err := orm.DB.WithContext(ctx).Model(&models.Repository{}).
			Where("owner_id = ? AND owner_type = ?", ownerID, ownerType).
			Pluck("id", &repoIDs).Error; err != nil {
			return nil, 0, status.Errorf(codes.Internal, "failed to list repositories: %v", err)
		}
		if len(repoIDs) == 0 {
			return []models.PullRequest{}, 0, nil
		}
		q = q.Where("repository_id IN ?", repoIDs)
	}
	if state != nil && *state != "all" {
		q = q.Where("state = ?", *state)
	}
	if author != nil {
		var u models.User
		if orm.DB.WithContext(ctx).Where("username = ?", *author).First(&u).Error == nil {
			q = q.Where("author_id = ?", u.ID)
		}
	}
	if base != nil {
		q = q.Where("base_branch = ?", *base)
	}
	if label != nil {
		q = q.Joins("JOIN pr_labels ON pr_labels.pull_request_id = pull_requests.id").
			Joins("JOIN labels ON labels.id = pr_labels.label_id").
			Where("labels.name = ?", *label)
	}
	if assignee != nil {
		q = q.Joins("JOIN pr_assignees ON pr_assignees.pull_request_id = pull_requests.id").
			Joins("JOIN users ON users.id = pr_assignees.user_id").
			Where("users.username = ?", *assignee)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, status.Errorf(codes.Internal, "failed to count pull requests: %v", err)
	}
	var prs []models.PullRequest
	if err := q.Offset((page - 1) * perPage).Limit(perPage).Find(&prs).Error; err != nil {
		return nil, 0, status.Errorf(codes.Internal, "failed to list pull requests: %v", err)
	}
	return prs, total, nil
}

func (s *PRService) UpdatePullRequest(ctx context.Context, callerID uint, owner, repo string, number int, title, body, base *string) (*models.PullRequest, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	pr, err := s.loadPRBase(ctx, r.ID, number)
	if err != nil {
		return nil, err
	}
	if title != nil {
		pr.Title = *title
	}
	if body != nil {
		pr.Body = *body
	}
	if base != nil {
		pr.BaseBranch = *base
	}
	if err := orm.DB.WithContext(ctx).Save(pr).Error; err != nil {
		return nil, status.Errorf(codes.Internal, "failed to update pull request: %v", err)
	}
	return s.loadPRFull(ctx, r.ID, number)
}

func (s *PRService) MergePullRequest(ctx context.Context, callerID uint, owner, repo string, number int, mergeMethod, commitTitle, commitMessage *string) (bool, string, string, error) {
	dbCtx, dbCancel := context.WithTimeout(ctx, 5*time.Second)
	defer dbCancel()

	r, err := resolveRepo(dbCtx, owner, repo)
	if err != nil {
		return false, "", "", err
	}
	pr, err := s.loadPRBase(dbCtx, r.ID, number)
	if err != nil {
		return false, "", "", err
	}
	if pr.State != "open" {
		return false, "", "", status.Error(codes.FailedPrecondition, "pull request is not open")
	}

	// Check branch protection rules on the base branch.
	bpSvc := &BranchProtectionService{}
	rule := bpSvc.MatchingRule(r.ID, pr.BaseBranch)
	if rule != nil {
		if rule.RequirePullRequest {
			// Count approved reviews (non-dismissed).
			var approvedCount int64
			if err := orm.DB.WithContext(dbCtx).Model(&models.PRReview{}).
				Where("pull_request_id = ? AND state = 'APPROVED' AND dismissed = false", pr.ID).
				Count(&approvedCount).Error; err != nil {
				return false, "", "", status.Errorf(codes.Internal, "failed to count reviews: %v", err)
			}
			if int(approvedCount) < rule.RequiredApprovals {
				return false, "", "", status.Errorf(codes.FailedPrecondition,
					"branch protection requires at least %d approved review(s); got %d",
					rule.RequiredApprovals, approvedCount)
			}
			// Check if any non-dismissed CHANGES_REQUESTED review exists.
			var blockedCount int64
			if err := orm.DB.WithContext(dbCtx).Model(&models.PRReview{}).
				Where("pull_request_id = ? AND state = 'CHANGES_REQUESTED' AND dismissed = false", pr.ID).
				Count(&blockedCount).Error; err != nil {
				return false, "", "", status.Errorf(codes.Internal, "failed to count blocked reviews: %v", err)
			}
			if blockedCount > 0 {
				return false, "", "", status.Error(codes.FailedPrecondition,
					"branch protection blocks merge: reviewer requested changes")
			}
		}
	}

	// Resolve the caller's display name and email for the merge commit.
	committerName := "Gyt"
	committerEmail := "noreply@gyt.local"
	var caller models.User
	if callerID != 0 {
		if err := orm.DB.WithContext(dbCtx).First(&caller, callerID).Error; err == nil {
			committerName = caller.DisplayName
			if committerName == "" {
				committerName = caller.Username
			}
			committerEmail = caller.Email
		}
	}

	method := "merge"
	if mergeMethod != nil && *mergeMethod != "" {
		method = *mergeMethod
	}
	title := "Merge pull request #" + itoa(pr.Number) + " from " + pr.HeadBranch
	if commitTitle != nil && *commitTitle != "" {
		title = *commitTitle
	}

	// Perform the actual git merge via soft-serve with a longer timeout.
	gitCtx, gitCancel := context.WithTimeout(ctx, 15*time.Second)
	defer gitCancel()
	mergeResp, err := gitClient.GitClient.MergeBranches(gitCtx, &ssgrpc.MergeBranchesRequest{
		RepoName:       r.GitRepoName,
		BaseBranch:     pr.BaseBranch,
		HeadBranch:     pr.HeadBranch,
		MergeMethod:    method,
		CommitTitle:    title,
		CommitterName:  committerName,
		CommitterEmail: committerEmail,
	})
	if err != nil {
		return false, "", "", err
	}
	if !mergeResp.GetMerged() {
		return false, mergeResp.GetSha(), mergeResp.GetMessage(), nil
	}

	sha := mergeResp.GetSha()
	now := time.Now()
	writeCtx, writeCancel := context.WithTimeout(ctx, 5*time.Second)
	defer writeCancel()
	if err := orm.DB.WithContext(writeCtx).Model(pr).Updates(map[string]interface{}{
		"state":     "merged",
		"merged":    true,
		"merged_at": now,
		"head_sha":  sha,
	}).Error; err != nil {
		return false, "", "", status.Errorf(codes.Internal, "failed to update PR state: %v", err)
	}
	// Invalidate branch list, stats, and all content cached under the base branch,
	// since a merge creates a new commit on it. Also clear default-ref (no-ref) caches.
	cache.Delete(ctx,
		fmt.Sprintf("gyt:branches:%s/%s", owner, repo),
		fmt.Sprintf("gyt:stats:%s/%s", owner, repo),
	)
	cache.InvalidatePattern(ctx, fmt.Sprintf("gyt:commits:%s/%s:%s:*", owner, repo, pr.BaseBranch))
	cache.InvalidatePattern(ctx, fmt.Sprintf("gyt:commits:%s/%s::*", owner, repo))
	cache.InvalidatePattern(ctx, fmt.Sprintf("gyt:tree:%s/%s:%s:*", owner, repo, pr.BaseBranch))
	cache.InvalidatePattern(ctx, fmt.Sprintf("gyt:blob:%s/%s:%s:*", owner, repo, pr.BaseBranch))
	cache.InvalidatePattern(ctx, fmt.Sprintf("gyt:tree:%s/%s::*", owner, repo))
	cache.InvalidatePattern(ctx, fmt.Sprintf("gyt:blob:%s/%s::*", owner, repo))
	return true, sha, title, nil
}

func (s *PRService) ClosePullRequest(ctx context.Context, callerID uint, owner, repo string, number int) (*models.PullRequest, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	if err := orm.DB.WithContext(ctx).Model(&models.PullRequest{}).Where("repository_id = ? AND number = ?", r.ID, number).
		Update("state", "closed").Error; err != nil {
		return nil, status.Errorf(codes.Internal, "failed to close pull request: %v", err)
	}
	return s.loadPRFull(ctx, r.ID, number)
}

func (s *PRService) ReopenPullRequest(ctx context.Context, callerID uint, owner, repo string, number int) (*models.PullRequest, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	if err := orm.DB.WithContext(ctx).Model(&models.PullRequest{}).Where("repository_id = ? AND number = ?", r.ID, number).
		Updates(map[string]interface{}{"state": "open", "merged": false, "merged_at": nil}).Error; err != nil {
		return nil, status.Errorf(codes.Internal, "failed to reopen pull request: %v", err)
	}
	return s.loadPRFull(ctx, r.ID, number)
}

func (s *PRService) AddLabel(ctx context.Context, owner, repo string, number int, labelName string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return err
	}
	pr, err := s.loadPRBase(ctx, r.ID, number)
	if err != nil {
		return err
	}
	var label models.Label
	if err := orm.DB.WithContext(ctx).Where("repository_id = ? AND name = ?", r.ID, labelName).First(&label).Error; err != nil {
		return status.Errorf(codes.NotFound, "label %q not found", labelName)
	}
	return orm.DB.WithContext(ctx).Model(pr).Association("Labels").Append(&label)
}

func (s *PRService) RemoveLabel(ctx context.Context, owner, repo string, number int, labelName string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return err
	}
	pr, err := s.loadPRBase(ctx, r.ID, number)
	if err != nil {
		return err
	}
	var label models.Label
	if err := orm.DB.WithContext(ctx).Where("repository_id = ? AND name = ?", r.ID, labelName).First(&label).Error; err != nil {
		return status.Errorf(codes.NotFound, "label %q not found", labelName)
	}
	return orm.DB.WithContext(ctx).Model(pr).Association("Labels").Delete(&label)
}

func (s *PRService) AddAssignee(ctx context.Context, owner, repo string, number int, username string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return err
	}
	pr, err := s.loadPRBase(ctx, r.ID, number)
	if err != nil {
		return err
	}
	var user models.User
	if err := orm.DB.WithContext(ctx).Where("username = ?", username).First(&user).Error; err != nil {
		return status.Errorf(codes.NotFound, "user %q not found", username)
	}
	return orm.DB.WithContext(ctx).Model(pr).Association("Assignees").Append(&user)
}

func (s *PRService) RemoveAssignee(ctx context.Context, owner, repo string, number int, username string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return err
	}
	pr, err := s.loadPRBase(ctx, r.ID, number)
	if err != nil {
		return err
	}
	var user models.User
	if err := orm.DB.WithContext(ctx).Where("username = ?", username).First(&user).Error; err != nil {
		return status.Errorf(codes.NotFound, "user %q not found", username)
	}
	return orm.DB.WithContext(ctx).Model(pr).Association("Assignees").Delete(&user)
}

func (s *PRService) CreateComment(ctx context.Context, callerID uint, owner, repo string, number int, body string, path *string, line *int) (*models.PRComment, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return nil, err
	}

	pr, err := s.loadPRBase(ctx, r.ID, number)
	if err != nil {
		return nil, err
	}

	comment := &models.PRComment{
		PullRequestID: pr.ID,
		AuthorID:      callerID,
		Body:          body,
		Path:          path,
		Line:          line,
	}

	if err := orm.DB.WithContext(ctx).Create(comment).Error; err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create comment: %v", err)
	}

	if err := orm.DB.WithContext(ctx).
		Preload("Author").
		First(comment, comment.ID).Error; err != nil {
		return nil, status.Errorf(codes.Internal, "failed to load comment: %v", err)
	}

	return comment, nil
}

func (s *PRService) ListComments(ctx context.Context, owner, repo string, number int) ([]models.PRComment, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return nil, err
	}

	pr, err := s.loadPRBase(ctx, r.ID, number)
	if err != nil {
		return nil, err
	}

	var comments []models.PRComment
	if err := orm.DB.WithContext(ctx).
		Where("pull_request_id = ?", pr.ID).
		Preload("Author").
		Order("created_at ASC").
		Find(&comments).Error; err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list comments: %v", err)
	}

	return comments, nil
}

func (s *PRService) UpdateComment(ctx context.Context, callerID uint, owner, repo string, commentID uint, body string) (*models.PRComment, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var comment models.PRComment
	if err := orm.DB.WithContext(ctx).Preload("Author").First(&comment, commentID).Error; err != nil {
		return nil, status.Errorf(codes.NotFound, "comment not found")
	}
	if comment.AuthorID != callerID {
		return nil, status.Error(codes.PermissionDenied, "cannot edit another user's comment")
	}
	comment.Body = body
	if err := orm.DB.WithContext(ctx).Save(&comment).Error; err != nil {
		return nil, status.Errorf(codes.Internal, "failed to update comment: %v", err)
	}
	return &comment, nil
}

func (s *PRService) DeleteComment(ctx context.Context, callerID uint, owner, repo string, commentID uint) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var comment models.PRComment
	if err := orm.DB.WithContext(ctx).First(&comment, commentID).Error; err != nil {
		return status.Errorf(codes.NotFound, "comment not found")
	}
	if comment.AuthorID != callerID {
		return status.Error(codes.PermissionDenied, "cannot delete another user's comment")
	}
	if err := orm.DB.WithContext(ctx).Delete(&comment).Error; err != nil {
		return status.Errorf(codes.Internal, "failed to delete comment: %v", err)
	}
	return nil
}

func (s *PRService) CreateReview(ctx context.Context, callerID uint, owner, repo string, number int, state, body string) (*models.PRReview, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return nil, err
	}

	pr, err := s.loadPRBase(ctx, r.ID, number)
	if err != nil {
		return nil, err
	}

	review := &models.PRReview{
		PullRequestID: pr.ID,
		ReviewerID:    callerID,
		State:         state,
		Body:          body,
	}

	if err := orm.DB.WithContext(ctx).Create(review).Error; err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create review: %v", err)
	}

	if err := orm.DB.WithContext(ctx).
		Preload("Reviewer").
		First(review, review.ID).Error; err != nil {
		return nil, status.Errorf(codes.Internal, "failed to load review: %v", err)
	}

	return review, nil
}
func (s *PRService) ListReviews(ctx context.Context, owner, repo string, number int) ([]models.PRReview, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return nil, err
	}

	pr, err := s.loadPRBase(ctx, r.ID, number)
	if err != nil {
		return nil, err
	}

	var reviews []models.PRReview
	if err := orm.DB.WithContext(ctx).
		Where("pull_request_id = ?", pr.ID).
		Preload("Reviewer").
		Order("created_at ASC").
		Find(&reviews).Error; err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list reviews: %v", err)
	}

	return reviews, nil
}

// ─── Review Requests ──────────────────────────────────────────────────────────

func (s *PRService) RequestReview(ctx context.Context, callerID uint, owner, repo string, number int, username string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return err
	}
	pr, err := s.loadPRBase(ctx, r.ID, number)
	if err != nil {
		return err
	}
	var reviewer models.User
	if err := orm.DB.WithContext(ctx).Where("username = ?", username).First(&reviewer).Error; err != nil {
		return status.Errorf(codes.NotFound, "user %q not found", username)
	}
	// Upsert: avoid duplicate requests.
	var existing models.ReviewRequest
	res := orm.DB.WithContext(ctx).Where("pull_request_id = ? AND reviewer_id = ?", pr.ID, reviewer.ID).First(&existing)
	if res.Error == gorm.ErrRecordNotFound {
		req := &models.ReviewRequest{
			PullRequestID: pr.ID,
			ReviewerID:    reviewer.ID,
			RequestedByID: callerID,
		}
		if err := orm.DB.WithContext(ctx).Create(req).Error; err != nil {
			return status.Errorf(codes.Internal, "failed to create review request: %v", err)
		}
	}
	return nil
}

func (s *PRService) RemoveReviewRequest(ctx context.Context, callerID uint, owner, repo string, number int, username string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return err
	}
	pr, err := s.loadPRBase(ctx, r.ID, number)
	if err != nil {
		return err
	}
	var reviewer models.User
	if err := orm.DB.WithContext(ctx).Where("username = ?", username).First(&reviewer).Error; err != nil {
		return status.Errorf(codes.NotFound, "user %q not found", username)
	}
	if err := orm.DB.WithContext(ctx).Where("pull_request_id = ? AND reviewer_id = ?", pr.ID, reviewer.ID).Delete(&models.ReviewRequest{}).Error; err != nil {
		return status.Errorf(codes.Internal, "failed to remove review request: %v", err)
	}
	return nil
}

func (s *PRService) ListReviewRequests(ctx context.Context, owner, repo string, number int) ([]models.ReviewRequest, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return nil, err
	}

	pr, err := s.loadPRBase(ctx, r.ID, number)
	if err != nil {
		return nil, err
	}

	var requests []models.ReviewRequest
	if err := orm.DB.WithContext(ctx).
		Where("pull_request_id = ?", pr.ID).
		Preload("Reviewer").
		Preload("RequestedBy").
		Find(&requests).Error; err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list review requests: %v", err)
	}

	return requests, nil
}

// ─── Review Dismissal ─────────────────────────────────────────────────────────

func (s *PRService) DismissReview(ctx context.Context, callerID uint, owner, repo, reviewID, reason string) (*models.PRReview, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	id, err := strconv.ParseUint(reviewID, 10, 64)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid review id")
	}
	// Verify the review belongs to a PR in this repo.
	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	var review models.PRReview
	if err := orm.DB.WithContext(ctx).Preload("Reviewer").Preload("PullRequest").First(&review, id).Error; err != nil {
		return nil, status.Error(codes.NotFound, "review not found")
	}
	if review.PullRequest.RepositoryID != r.ID {
		return nil, status.Error(codes.PermissionDenied, "review does not belong to this repository")
	}
	now := time.Now()
	review.Dismissed = true
	review.DismissedAt = &now
	review.DismissReason = reason
	review.State = "DISMISSED"
	if err := orm.DB.WithContext(ctx).Save(&review).Error; err != nil {
		return nil, status.Errorf(codes.Internal, "failed to dismiss review: %v", err)
	}
	return &review, nil
}

// DismissStaleReviews dismisses all non-dismissed APPROVED/CHANGES_REQUESTED reviews on a PR.
// This should be called when new commits are pushed to the head branch.
func (s *PRService) DismissStaleReviews(ctx context.Context, callerID uint, owner, repo string, number int) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return err
	}
	pr, err := s.loadPRBase(ctx, r.ID, number)
	if err != nil {
		return err
	}
	// Only auto-dismiss if branch protection says so.
	bpSvc := &BranchProtectionService{}
	rule := bpSvc.MatchingRule(r.ID, pr.BaseBranch)
	if rule == nil || !rule.DismissStaleReviews {
		return nil
	}
	now := time.Now()
	return orm.DB.WithContext(ctx).Model(&models.PRReview{}).
		Where("pull_request_id = ? AND dismissed = false AND state IN ('APPROVED', 'CHANGES_REQUESTED')", pr.ID).
		Updates(map[string]interface{}{
			"dismissed":      true,
			"dismissed_at":   now,
			"dismiss_reason": "New commits were pushed to the branch",
			"state":          "DISMISSED",
		}).Error
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func (s *PRService) attachPRAssignees(prID uint, usernames []string) {
	for _, u := range usernames {
		var user models.User
		if orm.DB.Where("username = ?", u).First(&user).Error == nil {
			orm.DB.Exec("INSERT INTO pr_assignees (pull_request_id, user_id) VALUES (?, ?) ON CONFLICT DO NOTHING", prID, user.ID)
		}
	}
}

func (s *PRService) attachPRLabels(prID, repoID uint, names []string) {
	for _, n := range names {
		var label models.Label
		if orm.DB.Where("repository_id = ? AND name = ?", repoID, n).First(&label).Error == nil {
			orm.DB.Exec("INSERT INTO pr_labels (pull_request_id, label_id) VALUES (?, ?) ON CONFLICT DO NOTHING", prID, label.ID)
		}
	}
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}
