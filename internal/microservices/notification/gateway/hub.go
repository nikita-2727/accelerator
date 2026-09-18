package gateway

import (
	"log/slog"

	"accelerator/internal/microservices/notification/domain"
)

// Hub отвечает за управление всеми подключенными клиентами и рассылку уведомлений.
// работает через каналы  регистрации, отписки и пуша уведомлений
// канал представляет собой очередь сообщений, которые нужно обработать в основном цикле хаба
type Hub struct {
	// подключенные клиенты, ключ - userID
	clients map[string]*Client

	register   chan *Client
	unregister chan *Client
	push       chan PushRequest
}

// пуш запрос для отправки уведомления конкретному пользователю. отправляется в канал push хаба
type PushRequest struct {
	UserID       string
	Notification domain.Notification
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[string]*Client),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		push:       make(chan PushRequest, 256), // 
	}
}

// Run запускает основной цикл хаба, который обрабатывает регистрацию, отписку и пуш уведомлений
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			// если уже есть клиент с таким userID (реконнект), разрываем старое соединение и заменяем его новым
			if oldClient, exists := h.clients[client.userID]; exists {
				slog.Info("Replacing existing WebSocket connection", "user_id", client.userID)
				oldClient.unregister <- true
			}
			// регистрируем клиента
			h.clients[client.userID] = client
			slog.Info("Client connected", "user_id", client.userID, "total_clients", len(h.clients))

		case client := <-h.unregister:
			// если клиент в канале unregisted - удаляем его из списка и закрываем канал отправки
			if _, exists := h.clients[client.userID]; exists {
				delete(h.clients, client.userID)
				close(client.send)
				slog.Info("Client disconnected", "user_id", client.userID, "total_clients", len(h.clients))
			}

		case req := <-h.push:
			// если есть запрос в канале push - отправляем уведомление в его канал send, иначе игнорируем (клиент оффлайн)
			if client, exists := h.clients[req.UserID]; exists {
				select {
				case client.send <- []byte(req.Notification.Payload): // отправляем уведомление в канал клиента
					slog.Info("Notification pushed to client", "user_id", req.UserID, "notification_id", req.Notification.ID)
				default:
					// канал отправки переполнен, разрываем соединение с клиентом
					slog.Warn("Client send buffer full, disconnecting", "user_id", req.UserID)
					client.unregister <- true
				}
			}
		}
	}
}

// Register регистрирует нового клиента в хабе. 
// если клиент с таким userID уже существует, он будет заменен.
func (h *Hub) Register(client *Client) {
	select {
	case h.register <- client:
		// добавлен в очередь на регистрацию
	default:
		slog.Error("Hub register channel is full", "user_id", client.userID)
	}
}

// PushToUser отправляет уведомление конкретному пользователю. 
// если пользователь оффлайн - ничего не происходит
func (h *Hub) PushToUser(userID string, notification domain.Notification) {
	select {
	case h.push <- PushRequest{UserID: userID, Notification: notification}:
	default:
		slog.Error("Hub push channel is full, dropping notification", "user_id", userID)
	}
}
