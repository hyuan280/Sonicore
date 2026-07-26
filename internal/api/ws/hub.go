package ws

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Hub struct {
	mu      sync.RWMutex
	clients map[string][]*wsClient
}

type wsClient struct {
	conn *websocket.Conn
}

func NewHub() *Hub {
	return &Hub{
		clients: make(map[string][]*wsClient),
	}
}

func (h *Hub) Handle(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[ws] upgrade error: %v", err)
		return
	}

	channel := r.URL.Query().Get("channel")
	if channel == "" {
		channel = "default"
	}

	c := &wsClient{conn: conn}

	conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		return nil
	})

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}()

	h.mu.Lock()
	h.clients[channel] = append(h.clients[channel], c)
	h.mu.Unlock()
	log.Printf("[ws] connected: channel=%s (%d clients)", channel, len(h.clients[channel]))

	defer func() {
		h.mu.Lock()
		clients := h.clients[channel]
		for i, cc := range clients {
			if cc == c {
				h.clients[channel] = append(clients[:i], clients[i+1:]...)
				break
			}
		}
		h.mu.Unlock()
		conn.Close()
		log.Printf("[ws] disconnected: channel=%s", channel)
	}()

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			log.Printf("[ws] read error: channel=%s err=%v", channel, err)
			return
		}
	}
}

func (h *Hub) Broadcast(channel string, v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}

	h.mu.RLock()
	clients := h.clients[channel]
	h.mu.RUnlock()

	for _, c := range clients {
		c.conn.WriteMessage(websocket.TextMessage, data)
	}
}
