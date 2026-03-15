package service

import (
	"context"
	"time"

	"github.com/Gyt-project/backend-api/internal/orm"
	"github.com/Gyt-project/backend-api/pkg/models"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

// IssueService gère la logique métier des issues.
type IssueService struct{}

func (s *IssueService) nextIssueNumber(repoID uint) (int, error) {
	var max int
	row := orm.DB.Model(&models.Issue{}).Where("repository_id = ?", repoID).Select("COALESCE(MAX(number), 0)").Row()
	row.Scan(&max)
	return max + 1, nil
}

func (s *IssueService) loadIssue(repoID uint, number int) (*models.Issue, error) {
	var issue models.Issue
	err := orm.DB.Where("repository_id = ? AND number = ?", repoID, number).
		Preload("Author").
		Preload("Assignees").
		Preload("Labels").
		First(&issue).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, status.Errorf(codes.NotFound, "issue #%d not found", number)
		}
		return nil, status.Errorf(codes.Internal, "db error: %v", err)
	}
	return &issue, nil
}

func (s *IssueService) CreateIssue(ctx context.Context, callerID uint, owner, repo, title string, body *string, assignees, labels []string) (*models.Issue, error) {
	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	num, _ := s.nextIssueNumber(r.ID)
	b := ""
	if body != nil {
		b = *body
	}
	issue := &models.Issue{
		RepositoryID: r.ID,
		Number:       num,
		Title:        title,
		Body:         b,
		State:        "open",
		AuthorID:     callerID,
	}
	if err := orm.DB.Create(issue).Error; err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create issue: %v", err)
	}
	s.attachAssignees(issue.ID, assignees)
	s.attachLabels(issue.ID, r.ID, labels)
	loaded, err := s.loadIssue(r.ID, num)
	if err != nil {
		return nil, err
	}
	DispatchWebhook(r.ID, "issues", map[string]interface{}{
		"action":     "opened",
		"number":     loaded.Number,
		"title":      loaded.Title,
		"state":      loaded.State,
		"repository": owner + "/" + repo,
	})
	return loaded, nil
}

func (s *IssueService) GetIssue(ctx context.Context, owner, repo string, number int) (*models.Issue, error) {
	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	return s.loadIssue(r.ID, number)
}

func (s *IssueService) ListIssues(ctx context.Context, owner string, repo *string, state, label, assignee, author *string, page, perPage int) ([]models.Issue, int64, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 30
	}

	q := orm.DB.Model(&models.Issue{}).
		Preload("Author").Preload("Assignees").Preload("Labels")

	if repo != nil && *repo != "" {
		// Filtrer par repo spécifique
		r, err := resolveRepo(ctx, owner, *repo)
		if err != nil {
			return nil, 0, err
		}
		q = q.Where("repository_id = ?", r.ID)
	} else {
		// Tous les repos du owner
		rs := &RepoService{}
		ownerID, ownerType, _, err := rs.resolveOwner(ctx, owner)
		if err != nil {
			return nil, 0, err
		}
		var repoIDs []uint
		orm.DB.Model(&models.Repository{}).
			Where("owner_id = ? AND owner_type = ?", ownerID, ownerType).
			Pluck("id", &repoIDs)
		if len(repoIDs) == 0 {
			return []models.Issue{}, 0, nil
		}
		q = q.Where("repository_id IN ?", repoIDs)
	}
	if state != nil && *state != "all" {
		q = q.Where("state = ?", *state)
	}
	if author != nil {
		var u models.User
		if orm.DB.Where("username = ?", *author).First(&u).Error == nil {
			q = q.Where("author_id = ?", u.ID)
		}
	}
	if label != nil {
		q = q.Joins("JOIN issue_labels ON issue_labels.issue_id = issues.id").
			Joins("JOIN labels ON labels.id = issue_labels.label_id").
			Where("labels.name = ?", *label)
	}
	if assignee != nil {
		q = q.Joins("JOIN issue_assignees ON issue_assignees.issue_id = issues.id").
			Joins("JOIN users ON users.id = issue_assignees.user_id").
			Where("users.username = ?", *assignee)
	}
	var total int64
	q.Count(&total)
	var issues []models.Issue
	if err := q.Offset((page - 1) * perPage).Limit(perPage).Find(&issues).Error; err != nil {
		return nil, 0, status.Errorf(codes.Internal, "failed to list issues: %v", err)
	}
	return issues, total, nil
}

func (s *IssueService) UpdateIssue(ctx context.Context, callerID uint, owner, repo string, number int, title, body *string) (*models.Issue, error) {
	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	issue, err := s.loadIssue(r.ID, number)
	if err != nil {
		return nil, err
	}
	if title != nil {
		issue.Title = *title
	}
	if body != nil {
		issue.Body = *body
	}
	orm.DB.Save(issue)
	return s.loadIssue(r.ID, number)
}

func (s *IssueService) CloseIssue(ctx context.Context, callerID uint, owner, repo string, number int) (*models.Issue, error) {
	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	orm.DB.Model(&models.Issue{}).Where("repository_id = ? AND number = ?", r.ID, number).
		Updates(map[string]interface{}{"state": "closed", "closed_at": now})
	return s.loadIssue(r.ID, number)
}

func (s *IssueService) ReopenIssue(ctx context.Context, callerID uint, owner, repo string, number int) (*models.Issue, error) {
	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	orm.DB.Model(&models.Issue{}).Where("repository_id = ? AND number = ?", r.ID, number).
		Updates(map[string]interface{}{"state": "open", "closed_at": nil})
	return s.loadIssue(r.ID, number)
}

func (s *IssueService) AddLabel(ctx context.Context, owner, repo string, number int, labelName string) error {
	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return err
	}
	issue, err := s.loadIssue(r.ID, number)
	if err != nil {
		return err
	}
	var label models.Label
	if err := orm.DB.Where("repository_id = ? AND name = ?", r.ID, labelName).First(&label).Error; err != nil {
		return status.Errorf(codes.NotFound, "label %q not found", labelName)
	}
	return orm.DB.Model(issue).Association("Labels").Append(&label)
}

func (s *IssueService) RemoveLabel(ctx context.Context, owner, repo string, number int, labelName string) error {
	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return err
	}
	issue, err := s.loadIssue(r.ID, number)
	if err != nil {
		return err
	}
	var label models.Label
	if err := orm.DB.Where("repository_id = ? AND name = ?", r.ID, labelName).First(&label).Error; err != nil {
		return status.Errorf(codes.NotFound, "label %q not found", labelName)
	}
	return orm.DB.Model(issue).Association("Labels").Delete(&label)
}

func (s *IssueService) AddAssignee(ctx context.Context, owner, repo string, number int, username string) error {
	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return err
	}
	issue, err := s.loadIssue(r.ID, number)
	if err != nil {
		return err
	}
	var user models.User
	if err := orm.DB.Where("username = ?", username).First(&user).Error; err != nil {
		return status.Errorf(codes.NotFound, "user %q not found", username)
	}
	return orm.DB.Model(issue).Association("Assignees").Append(&user)
}

func (s *IssueService) RemoveAssignee(ctx context.Context, owner, repo string, number int, username string) error {
	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return err
	}
	issue, err := s.loadIssue(r.ID, number)
	if err != nil {
		return err
	}
	var user models.User
	if err := orm.DB.Where("username = ?", username).First(&user).Error; err != nil {
		return status.Errorf(codes.NotFound, "user %q not found", username)
	}
	return orm.DB.Model(issue).Association("Assignees").Delete(&user)
}

func (s *IssueService) CreateComment(ctx context.Context, callerID uint, owner, repo string, number int, body string) (*models.IssueComment, error) {
	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	issue, err := s.loadIssue(r.ID, number)
	if err != nil {
		return nil, err
	}
	comment := &models.IssueComment{IssueID: issue.ID, AuthorID: callerID, Body: body}
	if err := orm.DB.Create(comment).Error; err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create comment: %v", err)
	}
	orm.DB.Preload("Author").First(comment, comment.ID)
	return comment, nil
}

func (s *IssueService) ListComments(ctx context.Context, owner, repo string, number int) ([]models.IssueComment, error) {
	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	issue, err := s.loadIssue(r.ID, number)
	if err != nil {
		return nil, err
	}
	var comments []models.IssueComment
	orm.DB.Where("issue_id = ?", issue.ID).Preload("Author").Find(&comments)
	return comments, nil
}

func (s *IssueService) UpdateComment(ctx context.Context, callerID uint, owner, repo string, commentID uint, body string) (*models.IssueComment, error) {
	var comment models.IssueComment
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

func (s *IssueService) DeleteComment(ctx context.Context, callerID uint, owner, repo string, commentID uint) error {
	var comment models.IssueComment
	if err := orm.DB.First(&comment, commentID).Error; err != nil {
		return status.Errorf(codes.NotFound, "comment not found")
	}
	if comment.AuthorID != callerID {
		return status.Error(codes.PermissionDenied, "cannot delete another user's comment")
	}
	return orm.DB.Delete(&comment).Error
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func (s *IssueService) attachAssignees(issueID uint, usernames []string) {
	for _, u := range usernames {
		var user models.User
		if orm.DB.Where("username = ?", u).First(&user).Error == nil {
			orm.DB.Exec("INSERT INTO issue_assignees (issue_id, user_id) VALUES (?, ?) ON CONFLICT DO NOTHING", issueID, user.ID)
		}
	}
}

func (s *IssueService) attachLabels(issueID, repoID uint, names []string) {
	for _, n := range names {
		var label models.Label
		if orm.DB.Where("repository_id = ? AND name = ?", repoID, n).First(&label).Error == nil {
			orm.DB.Exec("INSERT INTO issue_labels (issue_id, label_id) VALUES (?, ?) ON CONFLICT DO NOTHING", issueID, label.ID)
		}
	}
}
