package service

import (
	"context"
	"encoding/json"
	"log/slog"

	"accelerator/internal/core/event"
	"accelerator/internal/microservices/notification/domain"
	"accelerator/internal/microservices/notification/gateway"
	"accelerator/internal/microservices/notification/repository"
)

type Service struct {
	repo    *repository.Repository
	gateway *gateway.Hub
}

func NewService(repo *repository.Repository, gateway *gateway.Hub) *Service {
	return &Service{
		repo:    repo,
		gateway: gateway,
	}
}

// вызывается в транспорте и обрабатывает событие, создавая уведомление и отправляя его в репозиторий и на вебсокет
func (s *Service) ProcessEvent(ctx context.Context, evt event.Event) error {
	// декодим доп инфу из уведомления, если есть
	payloadJSON, err := json.Marshal(evt.Payload)
	if err != nil {
		return err
	}

	// создание домена уведомления из события
	notification := domain.Notification{
		ID:        evt.ID,
		UserID:    evt.UserID,
		Type:      string(evt.Type),
		Payload:   string(payloadJSON),
		IsRead:    false,
		CreatedAt: evt.Timestamp,
	}

	// сохраняем уведомление в репозитории
	if err := s.repo.Save(ctx, notification); err != nil {
		return err
	}

	// пушим в вебсокет. онлайн юзер или оффлайн - решается внутри хаба
	// уведомление уже в базе данных и будет доступно при следующем запросе истории
	if s.gateway != nil {
		go s.gateway.PushToUser(evt.UserID, notification)
	}

	slog.Info("Notification processed", 
		"notification_id", notification.ID,
		"user_id", evt.UserID,
		"type", evt.Type,
	)

	return nil
}

// получение истории уведомлений для конкретного юзера с пагинацией 
func (s *Service) GetHistory(ctx context.Context, userID string, limit, offset int) ([]domain.Notification, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return s.repo.GetByUserID(ctx, userID, limit, offset)
}

// помечает уведомление как прочитанное
func (s *Service) MarkAsRead(ctx context.Context, notificationID, userID string) error {
	return s.repo.MarkAsRead(ctx, notificationID, userID)
}

// получает колво непрочитанных уведомлений для конкретного юзера
func (s *Service) GetUnreadCount(ctx context.Context, userID string) (int, error) {
	return s.repo.GetUnreadCount(ctx, userID)
}