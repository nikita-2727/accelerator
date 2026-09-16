package event

import (
	"time"

	"github.com/google/uuid"
)

type EventType string

const (
	TaskCompleted   EventType = "task.completed"
	TaskFailed      EventType = "task.failed"
)

type Event struct {
	ID        string                 `json:"id"`
	Type      EventType              `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	UserID    string                 `json:"user_id"`
	Payload   map[string]interface{} `json:"payload"`
}

func NewEvent(eventType EventType, userID string, payload map[string]interface{}) Event {
	return Event{
		ID:        uuid.New().String(),
		Type:      eventType,
		Timestamp: time.Now().UTC(),
		UserID:    userID,
		Payload:   payload,
	}
}

// ================ Конструкторы кастомных ивентов ================

func NewTaskCompletedEvent(userID, taskID, resultURL string) Event {
	return NewEvent(TaskCompleted, userID, map[string]interface{}{
		"result_url": resultURL,
		"message":    "Задача успешно завершена.",
	})
}

func NewTaskFailedEvent(userID, taskID, errorMessage string) Event {
	return NewEvent(TaskFailed, userID, map[string]interface{}{
		"error_message": errorMessage,
		"message":       "Задача завершилась с ошибкой.",
	})
}