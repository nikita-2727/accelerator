package gateway

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// время на запись одного сообщения в сокет.
	// если клиент не успевает принять сообщение за это время - разрываем соединение.
	writeWait = 10 * time.Second

	// время на ожидание pong от клиента после отправки ping.
	// если не получаем pong за это время - разрываем соединение.
	pongWait = 60 * time.Second

	// время на отправку ping клиенту. должно быть меньше pongWait, чтобы успеть получить pong до истечения pongWait.
	// отправляем ping каждые 54 секунды, чтобы убедиться, что соединение живое.
	pingPeriod = (pongWait * 9) / 10

	// максимальное количество байт в одном сообщении от клиента.
	maxMessageSize = 512
)

var Upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// фикс для CORS: разрешаем подключение с любого источника. 
	// в продакшене лучше ограничить список доверенных источников.
	CheckOrigin: func(r *http.Request) bool { return true }, 
}

type Client struct {
	hub        *Hub
	conn       *websocket.Conn
	send       chan []byte
	userID     string
	unregister chan bool // сигнал для разрыва соединения
}

func NewClient(hub *Hub, conn *websocket.Conn, userID string) *Client {
	return &Client{
		hub:        hub,
		conn:       conn,
		send:       make(chan []byte, 256),
		userID:     userID,
		unregister: make(chan bool),
	}
}

// readPump отправляет сообщения от клиента в хаб
func (c *Client) ReadPump() {
	// закрываем соединение и отписываемся от хаба при выходе из функции
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	
	// SetPongHandler продливает время ожидания, при получении pong от клиента
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	// читаем сообщения от клиента в бесконечном цикле
	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				slog.Warn("WebSocket read error", "user_id", c.userID, "err", err)
			}
			break
		}
	}
}

// writePump отправляет сообщения из хаба клиенту
// периодически отправляет ping, чтобы убедиться, что соединение живое
func (c *Client) WritePump() {
	// таймер
	ticker := time.NewTicker(pingPeriod)
	// закрываем соединение и останавливаем таймер при выходе из функции
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// хаб закрыл канал, значит соединение нужно закрыть
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			// пишем сообщение в сокет
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				slog.Warn("WebSocket write error", "user_id", c.userID, "err", err)
				return
			}

		case <-ticker.C:
			// отпраляем ping клиенту, чтобы убедиться, что соединение живое
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				slog.Warn("WebSocket ping error", "user_id", c.userID, "err", err)
				return
			}
			
		case <-c.unregister:
			return
		}
	}
}