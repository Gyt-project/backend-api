package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Gyt-project/backend-api/internal/orm"
	"github.com/Gyt-project/backend-api/pkg/models"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

// PRService gère la logique métier des Pull Requests.
type PRService struct{}

func (s *PRService) nextPRNumber(repoID uint) int {
	var max int
	row := orm.DB.Model(&models.PullRequest{}).Where("repository_id = ?", repoID).Select("COALESCE(MAX(number), 0)").Row()
	row.Scan(&max)
	return max + 1
}

func (s *PRService) loadPR(repoID uint, number int) (*models.PullRequest, error) {
	var pr models.PullRequest
	err := orm.DB.Where("repository_id = ? AND number = ?", repoID, number).
		Preload("Author").Preload("Assignees").Preload("Labels").
		Preload("Comments.Author").Preload("Reviews.Reviewer").
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
	pr := &models.PullRequest{
		RepositoryID: r.ID,
		Number:       s.nextPRNumber(r.ID),
		Title:        title,
		Body:         b,
		State:        "open",
		HeadBranch:   head,
		BaseBranch:   base,
		HeadSHA:      "", // sera rempli par un webhook git ou une action future
		AuthorID:     callerID,
		Mergeable:    true,
	}
	if err := orm.DB.Create(pr).Error; err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create pull request: %v", err)
	}
	s.attachPRAssignees(pr.ID, assignees)
	s.attachPRLabels(pr.ID, r.ID, labels)
	return s.loadPR(r.ID, pr.Number)
}

func (s *PRService) GetPullRequest(ctx context.Context, owner, repo string, number int) (*models.PullRequest, error) {
	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	return s.loadPR(r.ID, number)
}

func (s *PRService) ListPullRequests(ctx context.Context, owner, repo string, state, author, assignee, label, base *string, page, perPage int) ([]models.PullRequest, int64, error) {
	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 30
	}
	q := orm.DB.Model(&models.PullRequest{}).Where("repository_id = ?", r.ID).
		Preload("Author").Preload("Assignees").Preload("Labels")
	if state != nil && *state != "all" {
		q = q.Where("state = ?", *state)
	}
	if author != nil {
		var u models.User
		if orm.DB.Where("username = ?", *author).First(&u).Error == nil {
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
	q.Count(&total)
	var prs []models.PullRequest
	if err := q.Offset((page - 1) * perPage).Limit(perPage).Find(&prs).Error; err != nil {
		return nil, 0, status.Errorf(codes.Internal, "failed to list pull requests: %v", err)
	}
	return prs, total, nil
}

func (s *PRService) UpdatePullRequest(ctx context.Context, callerID uint, owner, repo string, number int, title, body, base *string) (*models.PullRequest, error) {
	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	pr, err := s.loadPR(r.ID, number)
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
	orm.DB.Save(pr)
	return s.loadPR(r.ID, number)
}

func (s *PRService) MergePullRequest(ctx context.Context, callerID uint, owner, repo string, number int, mergeMethod, commitTitle, commitMessage *string) (bool, string, string, error) {
	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return false, "", "", err
	}
	pr, err := s.loadPR(r.ID, number)
	if err != nil {
		return false, "", "", err
	}
	if pr.State != "open" {
		return false, "", "", status.Error(codes.FailedPrecondition, "pull request is not open")
	}
	now := time.Now()
	sha := pr.HeadSHA
	if sha == "" {
		sha = "merged"
	}
	title := "Merge pull request #" + itoa(pr.Number)
	if commitTitle != nil {
		title = *commitTitle
	}
	orm.DB.Model(pr).Updates(map[string]interface{}{
		"state":     "merged",
		"merged":    true,
		"merged_at": now,
	})
	return true, sha, title, nil
}

func (s *PRService) ClosePullRequest(ctx context.Context, callerID uint, owner, repo string, number int) (*models.PullRequest, error) {
	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	orm.DB.Model(&models.PullRequest{}).Where("repository_id = ? AND number = ?", r.ID, number).
		Update("state", "closed")
	return s.loadPR(r.ID, number)
}

func (s *PRService) ReopenPullRequest(ctx context.Context, callerID uint, owner, repo string, number int) (*models.PullRequest, error) {
	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	orm.DB.Model(&models.PullRequest{}).Where("repository_id = ? AND number = ?", r.ID, number).
		Updates(map[string]interface{}{"state": "open", "merged": false, "merged_at": nil})
	return s.loadPR(r.ID, number)
}

func (s *PRService) AddLabel(ctx context.Context, owner, repo string, number int, labelName string) error {
	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return err
	}
	pr, err := s.loadPR(r.ID, number)
	if err != nil {
		return err
	}
	var label models.Label
	if err := orm.DB.Where("repository_id = ? AND name = ?", r.ID, labelName).First(&label).Error; err != nil {
		return status.Errorf(codes.NotFound, "label %q not found", labelName)
	}
	return orm.DB.Model(pr).Association("Labels").Append(&label)
}

func (s *PRService) RemoveLabel(ctx context.Context, owner, repo string, number int, labelName string) error {
	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return err
	}
	pr, err := s.loadPR(r.ID, number)
	if err != nil {
		return err
	}
	var label models.Label
	if err := orm.DB.Where("repository_id = ? AND name = ?", r.ID, labelName).First(&label).Error; err != nil {
		return status.Errorf(codes.NotFound, "label %q not found", labelName)
	}
	return orm.DB.Model(pr).Association("Labels").Delete(&label)
}

func (s *PRService) AddAssignee(ctx context.Context, owner, repo string, number int, username string) error {
	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return err
	}
	pr, err := s.loadPR(r.ID, number)
	if err != nil {
		return err
	}
	var user models.User
	if err := orm.DB.Where("username = ?", username).First(&user).Error; err != nil {
		return status.Errorf(codes.NotFound, "user %q not found", username)
	}
	return orm.DB.Model(pr).Association("Assignees").Append(&user)
}

func (s *PRService) RemoveAssignee(ctx context.Context, owner, repo string, number int, username string) error {
	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return err
	}
	pr, err := s.loadPR(r.ID, number)
	if err != nil {
		return err
	}
	var user models.User
	if err := orm.DB.Where("username = ?", username).First(&user).Error; err != nil {
		return status.Errorf(codes.NotFound, "user %q not found", username)
	}
	return orm.DB.Model(pr).Association("Assignees").Delete(&user)
}

func (s *PRService) CreateComment(ctx context.Context, callerID uint, owner, repo string, number int, body string, path *string, line *int) (*models.PRComment, error) {
	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	pr, err := s.loadPR(r.ID, number)
	if err != nil {
		return nil, err
	}
	comment := &models.PRComment{PullRequestID: pr.ID, AuthorID: callerID, Body: body, Path: path, Line: line}
	if err := orm.DB.Create(comment).Error; err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create comment: %v", err)
	}
	orm.DB.Preload("Author").First(comment, comment.ID)
	return comment, nil
}

func (s *PRService) ListComments(ctx context.Context, owner, repo string, number int) ([]models.PRComment, error) {
	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	pr, err := s.loadPR(r.ID, number)
	if err != nil {
		return nil, err
	}
	var comments []models.PRComment
	orm.DB.Where("pull_request_id = ?", pr.ID).Preload("Author").Find(&comments)
	return comments, nil
}

func (s *PRService) UpdateComment(ctx context.Context, callerID uint, owner, repo string, commentID uint, body string) (*models.PRComment, error) {
	var comment models.PRComment
	if err := orm.DB.Preload("Author").First(&comment, commentID).Error; err != nil {
		return nil, status.Errorf(codes.NotFound, "comment not found")
	}
	if comment.AuthorID != callerID {
		return nil, status.Error(codes.PermissionDenied, "cannot edit another user's comment")
	}
	comment.Body = body
	orm.DB.Save(&comment)
	return &comment, nil
}

func (s *PRService) DeleteComment(ctx context.Context, callerID uint, owner, repo string, commentID uint) error {
	var comment models.PRComment
	if err := orm.DB.First(&comment, commentID).Error; err != nil {
		return status.Errorf(codes.NotFound, "comment not found")
	}
	if comment.AuthorID != callerID {
		return status.Error(codes.PermissionDenied, "cannot delete another user's comment")
	}
	return orm.DB.Delete(&comment).Error
}

func (s *PRService) CreateReview(ctx context.Context, callerID uint, owner, repo string, number int, state, body string) (*models.PRReview, error) {
	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	pr, err := s.loadPR(r.ID, number)
	if err != nil {
		return nil, err
	}
	review := &models.PRReview{PullRequestID: pr.ID, ReviewerID: callerID, State: state, Body: body}
	if err := orm.DB.Create(review).Error; err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create review: %v", err)
	}
	orm.DB.Preload("Reviewer").First(review, review.ID)
	return review, nil
}

func (s *PRService) ListReviews(ctx context.Context, owner, repo string, number int) ([]models.PRReview, error) {
	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	pr, err := s.loadPR(r.ID, number)
	if err != nil {
		return nil, err
	}
	var reviews []models.PRReview
	orm.DB.Where("pull_request_id = ?", pr.ID).Preload("Reviewer").Find(&reviews)
	return reviews, nil
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
