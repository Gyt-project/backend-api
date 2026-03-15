package service

import (
	"context"

	"github.com/Gyt-project/backend-api/internal/orm"
	"github.com/Gyt-project/backend-api/pkg/models"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// StarService gère les étoiles sur les dépôts.
type StarService struct{}

// StarRepository ajoute une étoile à un dépôt pour l'utilisateur connecté.
func (s *StarService) StarRepository(ctx context.Context, callerID uint, owner, name string) error {
	repo, err := resolveRepo(ctx, owner, name)
	if err != nil {
		return err
	}
	star := &models.Star{UserID: callerID, RepositoryID: repo.ID}
	if err := orm.DB.FirstOrCreate(star, star).Error; err != nil {
		return status.Errorf(codes.Internal, "failed to star repository: %v", err)
	}
	if err := orm.DB.Model(&models.Repository{}).Where("id = ?", repo.ID).
		UpdateColumn("stars", orm.DB.Raw("stars + 1")).Error; err != nil {
		return err
	}
	DispatchWebhook(repo.ID, "star", map[string]interface{}{
		"action":     "created",
		"repository": owner + "/" + name,
	})
	return nil
}

// UnstarRepository retire l'étoile d'un dépôt pour l'utilisateur connecté.
func (s *StarService) UnstarRepository(ctx context.Context, callerID uint, owner, name string) error {
	repo, err := resolveRepo(ctx, owner, name)
	if err != nil {
		return err
	}
	res := orm.DB.Where("user_id = ? AND repository_id = ?", callerID, repo.ID).
		Delete(&models.Star{})
	if res.Error != nil {
		return status.Errorf(codes.Internal, "failed to unstar repository: %v", res.Error)
	}
	if res.RowsAffected > 0 {
		orm.DB.Model(&models.Repository{}).Where("id = ?", repo.ID).
			UpdateColumn("stars", orm.DB.Raw("GREATEST(stars - 1, 0)"))
		DispatchWebhook(repo.ID, "star", map[string]interface{}{
			"action":     "deleted",
			"repository": owner + "/" + name,
		})
	}
	return nil
}

// CheckStar vérifie si l'utilisateur a mis une étoile sur le dépôt.
func (s *StarService) CheckStar(ctx context.Context, callerID uint, owner, name string) (bool, error) {
	repo, err := resolveRepo(ctx, owner, name)
	if err != nil {
		return false, err
	}
	var count int64
	orm.DB.Model(&models.Star{}).
		Where("user_id = ? AND repository_id = ?", callerID, repo.ID).
		Count(&count)
	return count > 0, nil
}

// ListStargazers liste les utilisateurs ayant mis une étoile sur un dépôt.
func (s *StarService) ListStargazers(ctx context.Context, owner, name string, page, perPage int) ([]models.User, int64, error) {
	repo, err := resolveRepo(ctx, owner, name)
	if err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 30
	}
	var users []models.User
	var total int64
	q := orm.DB.Model(&models.User{}).
		Joins("JOIN stars ON stars.user_id = users.id").
		Where("stars.repository_id = ?", repo.ID)
	q.Count(&total)
	if err := q.Offset((page - 1) * perPage).Limit(perPage).Find(&users).Error; err != nil {
		return nil, 0, status.Errorf(codes.Internal, "failed to list stargazers: %v", err)
	}
	return users, total, nil
}

// ListStarredRepositories liste les dépôts qu'un utilisateur a mis en étoile.
func (s *StarService) ListStarredRepositories(ctx context.Context, username string, page, perPage int) ([]models.Repository, int64, error) {
	var user models.User
	if err := orm.DB.Where("username = ?", username).First(&user).Error; err != nil {
		return nil, 0, status.Errorf(codes.NotFound, "user not found")
	}
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 30
	}
	var repos []models.Repository
	var total int64
	q := orm.DB.Model(&models.Repository{}).
		Joins("JOIN stars ON stars.repository_id = repositories.id").
		Where("stars.user_id = ?", user.ID)
	q.Count(&total)
	if err := q.Offset((page - 1) * perPage).Limit(perPage).Find(&repos).Error; err != nil {
		return nil, 0, status.Errorf(codes.Internal, "failed to list starred repos: %v", err)
	}
	return repos, total, nil
}

// resolveRepo est un helper local pour récupérer un dépôt par owner/name.
func resolveRepo(ctx context.Context, owner, name string) (*models.Repository, error) {
	rs := &RepoService{}
	ownerID, ownerType, _, err := rs.resolveOwner(ctx, owner)
	if err != nil {
		return nil, err
	}
	var repo models.Repository
	if err := orm.DB.Where("owner_id = ? AND owner_type = ? AND name = ?", ownerID, ownerType, name).
		First(&repo).Error; err != nil {
		return nil, status.Errorf(codes.NotFound, "repository not found")
	}
	return &repo, nil
}
