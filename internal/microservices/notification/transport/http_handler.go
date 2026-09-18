package transport

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"accelerator/internal/core/event"
	"accelerator/internal/core/server/authctx"
	"accelerator/internal/microservices/notification/service"
)

// имеет все эндпоинты для работы с уведомлениями
type Handler struct {
	service *service.Service
}

func NewHandler(service *service.Service) *Handler {
	return &Handler{service: service}
}

// принимает входящее событие и обрабатывает его, создавая уведомление и отправляя его в репозиторий и на вебсокет
func (h *Handler) HandleInternalEvent(w http.ResponseWriter, r *http.Request) {
	var evt event.Event
	if err := json.NewDecoder(r.Body).Decode(&evt); err != nil {
		slog.Warn("Invalid event payload", "err", err)
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
		// обрабатываем событие в отдельной горутине, чтобы не блокировать HTTP ответ что уведомление принято
		go func() {
		if err := h.service.ProcessEvent(r.Context(), evt); err != nil {
			slog.Error("Failed to process event", "event_id", evt.ID, "err", err)
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte(`{"status":"accepted"}`)) // ответ, что событие принято и будет обработано
}

// возвращает историю уведомлений для конкретного юзера с пагинацией
func (h *Handler) HandleGetHistory(w http.ResponseWriter, r *http.Request) {
	// получаем userID из контекста
	userID, ok := authctx.GetUserID(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	// выудиваем limit и offset для пагинации истории уведомлений из query параметров, если не указаны - дефолтные значения
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	// получаем историю уведомлений из бизнес логики
	notifications, err := h.service.GetHistory(r.Context(), userID, limit, offset)
	if err != nil {
		slog.Error("Failed to fetch notifications", "user_id", userID, "err", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(notifications)
}

// HandleMarkAsRead помечает конкретное уведомление как прочитанное для конкретного юзера
// POST /api/notifications/:id/read
func (h *Handler) HandleMarkAsRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := authctx.GetUserID(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	// выудиваем айди уведомления из URL
	notificationID := r.PathValue("id")
	if notificationID == "" {
		http.Error(w, `{"error":"missing id"}`, http.StatusBadRequest)
		return
	}

	if err := h.service.MarkAsRead(r.Context(), notificationID, userID); err != nil {
		slog.Error("Failed to mark as read", "notification_id", notificationID, "err", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleGetUnreadCount получает количество непрочитанных уведомлений для конкретного юзера
// GET /api/notifications/unread-count
func (h *Handler) HandleGetUnreadCount(w http.ResponseWriter, r *http.Request) {
	userID, ok := authctx.GetUserID(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	count, err := h.service.GetUnreadCount(r.Context(), userID)
	if err != nil {
		slog.Error("Failed to get unread count", "user_id", userID, "err", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"count": count})
}