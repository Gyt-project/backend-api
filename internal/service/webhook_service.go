package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

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

// PingWebhook sends a test ping payload to the webhook URL.
func (s *WebhookService) PingWebhook(ctx context.Context, owner string, repo *string, id uint) error {
	wh, err := s.GetWebhook(ctx, owner, repo, id)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"event":   "ping",
		"hook_id": wh.ID,
		"zen":     "Webhooks are great for automation.",
	})
	sendWebhookRequest(*wh, "ping", payload)
	return nil
}

// DispatchWebhook looks up active webhooks for repoID that subscribe to event
// and sends each one in a background goroutine.
func DispatchWebhook(repoID uint, event string, payload interface{}) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	var webhooks []models.Webhook
	orm.DB.Where("repository_id = ? AND active = ?", repoID, true).Find(&webhooks)
	for _, wh := range webhooks {
		if webhookMatchesEvent(wh, event) {
			wh := wh // capture loop variable
			go sendWebhookRequest(wh, event, data)
		}
	}
}

func webhookMatchesEvent(wh models.Webhook, event string) bool {
	var events []string
	json.Unmarshal([]byte(wh.Events), &events) //nolint:errcheck
	for _, e := range events {
		if e == event {
			return true
		}
	}
	return false
}

func sendWebhookRequest(wh models.Webhook, event string, payload []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, wh.URL, bytes.NewReader(payload))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gyt-Event", event)
	req.Header.Set("User-Agent", "Gyt-Webhooks/1.0")

	if wh.Secret != "" {
		mac := hmac.New(sha256.New, []byte(wh.Secret))
		mac.Write(payload)
		req.Header.Set("X-Gyt-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
}

// DecodeEvents désérialise la liste d'événements JSON stockée en base.
func DecodeEvents(raw string) []string {
	var events []string
	json.Unmarshal([]byte(raw), &events)
	return events
}
