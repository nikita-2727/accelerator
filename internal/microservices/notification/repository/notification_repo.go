package repository

import (
	"context"
	"log/slog"

	"accelerator/internal/microservices/notification/domain"
)

// todo: добавить бд

// Repository пока что моковый репозиторий.
// потом будет писать в бдшку для сохранения уведомлений и получения истории
type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}

// Save сохраняет уведомление в базе данных. 
// пока что моки
func (r *Repository) Save(ctx context.Context, notification domain.Notification) error {
	slog.Info("MOCK!!! Saving notification to database",
		"notification_id", notification.ID,
		"user_id", notification.UserID,
		"type", notification.Type,
		"payload", notification.Payload,
		"is_read", notification.IsRead,
		"created_at", notification.CreatedAt,
	)
	return nil
}

// GetByUserID возвращает список уведомлений для конкретного пользователя. 
// пока что моки
func (r *Repository) GetByUserID(ctx context.Context, userID string, limit, offset int) ([]domain.Notification, error) {
	slog.Info("MOCK!!! Fetching notifications from database",
		"user_id", userID,
		"limit", limit,
		"offset", offset,
	)
	return []domain.Notification{}, nil
}

// MarkAsRead помечает уведомление как прочитанное.
// пока что моки
func (r *Repository) MarkAsRead(ctx context.Context, notificationID, userID string) error {
	slog.Info("MOCK!!! Marking notification as read",
		"notification_id", notificationID,
		"user_id", userID,
	)
	return nil
}

// GetUnreadCount возвращает количество непрочитанных уведомлений для конкретного пользователя.
// пока что моки
func (r *Repository) GetUnreadCount(ctx context.Context, userID string) (int, error) {
	slog.Info("MOCK!!! Counting unread notifications",
		"user_id", userID,
	)
	return 0, nil
}