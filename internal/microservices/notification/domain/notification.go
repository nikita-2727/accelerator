package domain

import "time"

// единая структура для всех уведомлений, которые будут храниться в базе данных
type Notification struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Type      string    `json:"type"`    // например "task_completed", "new_message", "group_invite" и т.д.
	Payload   string    `json:"payload"` // доп инфа. парсится в json чтобы не хранить в базе кучу полей для каждого типа уведомления
	IsRead    bool      `json:"is_read"`
	CreatedAt time.Time `json:"created_at"`
}