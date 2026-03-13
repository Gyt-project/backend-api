package service

import (
	"context"
	"encoding/json"

	"github.com/Gyt-project/backend-api/internal/orm"
	"github.com/Gyt-project/backend-api/pkg/models"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// WebhookService gère les webhooks de dépôts et d'organisations.
type WebhookService struct{}

func (s *WebhookService) CreateWebhook(ctx context.Context, owner string, repo *string, url string, events []string, secret *string, active *bool, contentType *string) (*models.Webhook, error) {
	eventsJSON, _ := json.Marshal(events)
	wh := &models.Webhook{
		URL:         url,
		Events:      string(eventsJSON),
		ContentType: "json",
		Active:      true,
	}
	if secret != nil {
		wh.Secret = *secret
	}
	if active != nil {
		wh.Active = *active
	}
	if contentType != nil {
		wh.ContentType = *contentType
	}

	rs := &RepoService{}
	if repo != nil {
		r, err := resolveRepo(ctx, owner, *repo)
		if err != nil {
			return nil, err
		}
		wh.RepositoryID = &r.ID
		ownerID, ownerType, _, err := rs.resolveOwner(ctx, owner)
		if err != nil {
			return nil, err
		}
		wh.OwnerID = ownerID
		wh.OwnerType = ownerType
	} else {
		ownerID, ownerType, _, err := rs.resolveOwner(ctx, owner)
		if err != nil {
			return nil, err
		}
		wh.OwnerID = ownerID
		wh.OwnerType = ownerType
	}

	if err := orm.DB.Create(wh).Error; err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create webhook: %v", err)
	}
	return wh, nil
}

func (s *WebhookService) GetWebhook(ctx context.Context, owner string, repo *string, id uint) (*models.Webhook, error) {
	var wh models.Webhook
	if err := orm.DB.First(&wh, id).Error; err != nil {
		return nil, status.Errorf(codes.NotFound, "webhook not found")
	}
	return &wh, nil
}

func (s *WebhookService) ListWebhooks(ctx context.Context, owner string, repo *string) ([]models.Webhook, error) {
	rs := &RepoService{}
	ownerID, ownerType, _, err := rs.resolveOwner(ctx, owner)
	if err != nil {
		return nil, err
	}
	q := orm.DB.Model(&models.Webhook{}).Where("owner_id = ? AND owner_type = ?", ownerID, ownerType)
	if repo != nil {
		r, err := resolveRepo(ctx, owner, *repo)
		if err != nil {
			return nil, err
		}
		q = q.Where("repository_id = ?", r.ID)
	} else {
		q = q.Where("repository_id IS NULL")
	}
	var webhooks []models.Webhook
	if err := q.Find(&webhooks).Error; err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list webhooks: %v", err)
	}
	return webhooks, nil
}

func (s *WebhookService) UpdateWebhook(ctx context.Context, owner string, repo *string, id uint, url *string, events []string, active *bool, secret *string, contentType *string) (*models.Webhook, error) {
	wh, err := s.GetWebhook(ctx, owner, repo, id)
	if err != nil {
		return nil, err
	}
	if url != nil {
		wh.URL = *url
	}
	if len(events) > 0 {
		eventsJSON, _ := json.Marshal(events)
		wh.Events = string(eventsJSON)
	}
	if active != nil {
		wh.Active = *active
	}
	if secret != nil {
		wh.Secret = *secret
	}
	if contentType != nil {
		wh.ContentType = *contentType
	}
	orm.DB.Save(wh)
	return wh, nil
}

func (s *WebhookService) DeleteWebhook(ctx context.Context, owner string, repo *string, id uint) error {
	wh, err := s.GetWebhook(ctx, owner, repo, id)
	if err != nil {
		return err
	}
	return orm.DB.Delete(wh).Error
}

// PingWebhook est un no-op dans le cadre de l'implémentation de base.
// En production, il déclencherait un appel HTTP test vers l'URL du webhook.
func (s *WebhookService) PingWebhook(ctx context.Context, owner string, repo *string, id uint) error {
	_, err := s.GetWebhook(ctx, owner, repo, id)
	return err
}

// DecodeEvents désérialise la liste d'événements JSON stockée en base.
func DecodeEvents(raw string) []string {
	var events []string
	json.Unmarshal([]byte(raw), &events)
	return events
}

