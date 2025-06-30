package websocket

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"report-listener/models"
)

// Hub manages WebSocket connections and broadcasting
type Hub struct {
	// Registered clients
	clients map[*Client]bool

	// Inbound messages from clients
	broadcast chan []byte

	// Register requests from clients
	Register chan *Client

	// Unregister requests from clients
	Unregister chan *Client

	// Mutex for thread-safe operations
	mutex sync.RWMutex

	// Statistics
	lastBroadcastSeq int
	connectedClients int
}

// NewHub creates a new WebSocket hub
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte, 256),
		Register:   make(chan *Client),
		Unregister: make(chan *Client),
	}
}

// Run starts the hub's main loop
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.Register:
			h.mutex.Lock()
			h.clients[client] = true
			h.connectedClients = len(h.clients)
			h.mutex.Unlock()
			log.Printf("Client connected. Total clients: %d", h.connectedClients)

		case client := <-h.Unregister:
			h.mutex.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
				h.connectedClients = len(h.clients)
			}
			h.mutex.Unlock()
			log.Printf("Client disconnected. Total clients: %d", h.connectedClients)

		case message := <-h.broadcast:
			h.mutex.RLock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.connectedClients = len(h.clients)
			h.mutex.RUnlock()
		}
	}
}

// BroadcastReports broadcasts a batch of reports with analysis to all connected clients
func (h *Hub) BroadcastReports(reportsWithAnalysis []models.ReportWithAnalysis) {
	if len(reportsWithAnalysis) == 0 {
		return
	}

	// Update last broadcast sequence
	if len(reportsWithAnalysis) > 0 {
		h.lastBroadcastSeq = reportsWithAnalysis[len(reportsWithAnalysis)-1].Report.Seq
	}

	batch := models.ReportBatch{
		Reports: reportsWithAnalysis,
		Count:   len(reportsWithAnalysis),
		FromSeq: reportsWithAnalysis[0].Report.Seq,
		ToSeq:   reportsWithAnalysis[len(reportsWithAnalysis)-1].Report.Seq,
	}

	message := models.BroadcastMessage{
		Type:      "reports",
		Data:      batch,
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(message)
	if err != nil {
		log.Printf("Failed to marshal broadcast message: %v", err)
		return
	}

	h.broadcast <- data
	log.Printf("Broadcasted %d reports with analysis (seq %d-%d) to %d clients",
		len(reportsWithAnalysis), batch.FromSeq, batch.ToSeq, h.connectedClients)
}

// GetStats returns current hub statistics
func (h *Hub) GetStats() (int, int) {
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	return h.connectedClients, h.lastBroadcastSeq
}
