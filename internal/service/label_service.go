package service

import (
	"context"

	"github.com/Gyt-project/backend-api/internal/orm"
	"github.com/Gyt-project/backend-api/pkg/models"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// LabelService gère les labels d'un dépôt.
type LabelService struct{}

func (s *LabelService) CreateLabel(ctx context.Context, callerID uint, owner, repo, name, color string, description *string) (*models.Label, error) {
	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	desc := ""
	if description != nil {
		desc = *description
	}
	label := &models.Label{RepositoryID: r.ID, Name: name, Color: color, Description: desc}
	if err := orm.DB.Create(label).Error; err != nil {
		return nil, status.Errorf(codes.AlreadyExists, "label already exists or invalid: %v", err)
	}
	return label, nil
}

func (s *LabelService) GetLabel(ctx context.Context, owner, repo, name string) (*models.Label, error) {
	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	var label models.Label
	if err := orm.DB.Where("repository_id = ? AND name = ?", r.ID, name).First(&label).Error; err != nil {
		return nil, status.Errorf(codes.NotFound, "label not found")
	}
	return &label, nil
}

func (s *LabelService) ListLabels(ctx context.Context, owner, repo string) ([]models.Label, error) {
	r, err := resolveRepo(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	var labels []models.Label
	if err := orm.DB.Where("repository_id = ?", r.ID).Find(&labels).Error; err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list labels: %v", err)
	}
	return labels, nil
}

func (s *LabelService) UpdateLabel(ctx context.Context, callerID uint, owner, repo, name string, newName, color, description *string) (*models.Label, error) {
	label, err := s.GetLabel(ctx, owner, repo, name)
	if err != nil {
		return nil, err
	}
	if newName != nil {
		label.Name = *newName
	}
	if color != nil {
		label.Color = *color
	}
	if description != nil {
		label.Description = *description
	}
	if err := orm.DB.Save(label).Error; err != nil {
		return nil, status.Errorf(codes.Internal, "failed to update label: %v", err)
	}
	return label, nil
}

func (s *LabelService) DeleteLabel(ctx context.Context, callerID uint, owner, repo, name string) error {
	label, err := s.GetLabel(ctx, owner, repo, name)
	if err != nil {
		return err
	}
	return orm.DB.Delete(label).Error
}

