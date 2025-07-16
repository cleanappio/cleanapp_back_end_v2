package services

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"brand-dashboard/models"

	"github.com/gorilla/websocket"
)

// WebSocketHub manages WebSocket connections and broadcasts
type WebSocketHub struct {
	clients       map[*WebSocketClient]bool
	broadcast     chan models.BroadcastMessage
	register      chan *WebSocketClient
	unregister    chan *WebSocketClient
	mutex         sync.RWMutex
	lastBroadcast int
}

// WebSocketClient represents a WebSocket client connection
type WebSocketClient struct {
	hub    *WebSocketHub
	conn   *websocket.Conn
	send   chan []byte
	userID string
}

// NewWebSocketHub creates a new WebSocket hub
func NewWebSocketHub() *WebSocketHub {
	return &WebSocketHub{
		clients:    make(map[*WebSocketClient]bool),
		broadcast:  make(chan models.BroadcastMessage),
		register:   make(chan *WebSocketClient),
		unregister: make(chan *WebSocketClient),
	}
}

// Start starts the WebSocket hub
func (h *WebSocketHub) Start() {
	for {
		select {
		case client := <-h.register:
			h.mutex.Lock()
			h.clients[client] = true
			h.mutex.Unlock()
			log.Printf("INFO: WebSocket client registered for user %s", client.userID)

		case client := <-h.unregister:
			h.mutex.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mutex.Unlock()
			log.Printf("INFO: WebSocket client unregistered for user %s", client.userID)

		case message := <-h.broadcast:
			h.mutex.RLock()
			for client := range h.clients {
				select {
				case client.send <- h.serializeMessage(message):
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.mutex.RUnlock()
			h.lastBroadcast++
		}
	}
}

// Stop stops the WebSocket hub
func (h *WebSocketHub) Stop() {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	for client := range h.clients {
		close(client.send)
		delete(h.clients, client)
	}
}

// RegisterClient registers a new WebSocket client
func (h *WebSocketHub) RegisterClient(conn *websocket.Conn, userID string) {
	client := &WebSocketClient{
		hub:    h,
		conn:   conn,
		send:   make(chan []byte, 256),
		userID: userID,
	}

	h.register <- client

	// Start goroutines for reading and writing
	go client.writePump()
	go client.readPump()
}

// BroadcastMessage broadcasts a message to all connected clients
func (h *WebSocketHub) BroadcastMessage(message models.BroadcastMessage) {
	h.broadcast <- message
}

// GetConnectedClientsCount returns the number of connected clients
func (h *WebSocketHub) GetConnectedClientsCount() int {
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	return len(h.clients)
}

// GetLastBroadcastSeq returns the last broadcast sequence number
func (h *WebSocketHub) GetLastBroadcastSeq() int {
	return h.lastBroadcast
}

// serializeMessage serializes a broadcast message to JSON
func (h *WebSocketHub) serializeMessage(message models.BroadcastMessage) []byte {
	data, err := json.Marshal(message)
	if err != nil {
		log.Printf("ERROR: Failed to serialize message: %v", err)
		return []byte("{}")
	}
	return data
}

// readPump pumps messages from the WebSocket connection to the hub
func (c *WebSocketClient) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(512)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("ERROR: WebSocket read error for user %s: %v", c.userID, err)
			}
			break
		}

		// Handle incoming messages if needed
		log.Printf("DEBUG: Received message from user %s: %s", c.userID, string(message))
	}
}

// writePump pumps messages from the hub to the WebSocket connection
func (c *WebSocketClient) writePump() {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
