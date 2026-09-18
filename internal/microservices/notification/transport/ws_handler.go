package transport

import (
	"log/slog"
	"net/http"

	"accelerator/internal/core/server/authctx"
	"accelerator/internal/microservices/notification/gateway"
)

// todo: проблема в том что через вебсокет насколько я понял нельзя передавать токен в заголовке, 
// поэтому нужно будет придумать как передавать токен (мб в query параметре)

type WSHandler struct {
	hub *gateway.Hub
}

func NewWSHandler(hub *gateway.Hub) *WSHandler {
	return &WSHandler{hub: hub}
}

// HandleWebSocketUpgrade меняет подключение с http на websocket, 
// регистрирует клиента в хабе и запускает горутины для чтения и записи сообщений
func (h *WSHandler) HandleWebSocketUpgrade(w http.ResponseWriter, r *http.Request) {
	// аутентификация через контекст, получаем userID из контекста
	userID, ok := authctx.GetUserID(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized: missing user context"}`, http.StatusUnauthorized)
		return
	}

	// меняем подключение с http на websocket
	conn, err := gateway.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("Failed to upgrade WebSocket", "user_id", userID, "err", err)
		return
	}

	// создаем нового клиента и регистрируем его в хабе
	client := gateway.NewClient(h.hub, conn, userID)
	h.hub.Register(client)

	// начинает горутины для чтения и записи сообщений
	go client.WritePump()
	go client.ReadPump()

	slog.Info("WebSocket connection established", "user_id", userID)
}