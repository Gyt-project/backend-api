package service

import (
	"context"

	"github.com/Gyt-project/backend-api/internal/orm"
	"github.com/Gyt-project/backend-api/pkg/models"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// SearchService gère la recherche globale.
type SearchService struct{}

func (s *SearchService) SearchRepositories(ctx context.Context, query string, language, sort, order *string, page, perPage int) ([]models.Repository, int64, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 30
	}
	q := orm.DB.Model(&models.Repository{}).Where("name ILIKE ? OR description ILIKE ?", "%"+query+"%", "%"+query+"%")
	if language != nil {
		// placeholder : dans une implémentation future on filtrerait par langue
	}
	if sort != nil {
		switch *sort {
		case "stars":
			dir := "DESC"
			if order != nil && *order == "asc" {
				dir = "ASC"
			}
			q = q.Order("stars " + dir)
		case "forks":
			dir := "DESC"
			if order != nil && *order == "asc" {
				dir = "ASC"
			}
			q = q.Order("forks " + dir)
		default:
			q = q.Order("updated_at DESC")
		}
	} else {
		q = q.Order("stars DESC")
	}
	var total int64
	q.Count(&total)
	var repos []models.Repository
	if err := q.Offset((page - 1) * perPage).Limit(perPage).Find(&repos).Error; err != nil {
		return nil, 0, status.Errorf(codes.Internal, "search repos failed: %v", err)
	}
	return repos, total, nil
}

func (s *SearchService) SearchUsers(ctx context.Context, query string, sort, order *string, page, perPage int) ([]models.User, int64, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 30
	}
	q := orm.DB.Model(&models.User{}).Where("username ILIKE ? OR display_name ILIKE ?", "%"+query+"%", "%"+query+"%")
	if sort != nil && *sort == "repositories" {
		q = q.Order("(SELECT COUNT(*) FROM repositories WHERE owner_id = users.id AND owner_type = 'user') DESC")
	} else {
		q = q.Order("created_at DESC")
	}
	var total int64
	q.Count(&total)
	var users []models.User
	if err := q.Offset((page - 1) * perPage).Limit(perPage).Find(&users).Error; err != nil {
		return nil, 0, status.Errorf(codes.Internal, "search users failed: %v", err)
	}
	return users, total, nil
}

func (s *SearchService) SearchIssues(ctx context.Context, query string, state, issueType, owner, repo, author, label, sort, order *string, page, perPage int) ([]models.Issue, int64, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 30
	}
	q := orm.DB.Model(&models.Issue{}).
		Preload("Author").Preload("Assignees").Preload("Labels").
		Where("title ILIKE ? OR body ILIKE ?", "%"+query+"%", "%"+query+"%")
	if state != nil {
		q = q.Where("state = ?", *state)
	}
	if owner != nil && repo != nil {
		r, err := resolveRepo(ctx, *owner, *repo)
		if err == nil {
			q = q.Where("repository_id = ?", r.ID)
		}
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
	sortCol := "created_at"
	if sort != nil {
		switch *sort {
		case "updated":
			sortCol = "updated_at"
		case "comments":
			sortCol = "(SELECT COUNT(*) FROM issue_comments WHERE issue_id = issues.id)"
		}
	}
	dir := "DESC"
	if order != nil && *order == "asc" {
		dir = "ASC"
	}
	q = q.Order(sortCol + " " + dir)
	var total int64
	q.Count(&total)
	var issues []models.Issue
	if err := q.Offset((page - 1) * perPage).Limit(perPage).Find(&issues).Error; err != nil {
		return nil, 0, status.Errorf(codes.Internal, "search issues failed: %v", err)
	}
	return issues, total, nil
}

