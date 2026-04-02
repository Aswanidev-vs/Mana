package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Aswanidev-vs/mana/core"
	"github.com/Aswanidev-vs/mana/ws"
)

// Hub manages notification delivery in the Mana framework.
type Hub struct {
	mu     sync.RWMutex
	conns  map[string]map[string]ws.Conn
	logger Logger
}

// Logger interface for the notification hub.
type Logger interface {
	Info(format string, args ...interface{})
	Error(format string, args ...interface{})
}

// NewHub creates a new notification Hub.
func NewHub(logger Logger) *Hub {
	return &Hub{
		conns:  make(map[string]map[string]ws.Conn),
		logger: logger,
	}
}

// Register adds a user connection to the notification registry.
func (h *Hub) Register(userID, sessionID string, conn ws.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conns[userID] == nil {
		h.conns[userID] = make(map[string]ws.Conn)
	}
	h.conns[userID][sessionID] = conn
}

// Unregister removes a user from the notification registry.
func (h *Hub) Unregister(userID, sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conns[userID] == nil {
		return
	}
	delete(h.conns[userID], sessionID)
	if len(h.conns[userID]) == 0 {
		delete(h.conns, userID)
	}
}

// Send delivers a notification to a specific user.
func (h *Hub) Send(ctx context.Context, userID string, n core.Notification) error {
	h.mu.RLock()
	conns, ok := h.conns[userID]
	h.mu.RUnlock()

	if !ok {
		return fmt.Errorf("user %s not online for notifications", userID)
	}

	if n.Type == "" {
		n.Type = "notification"
	}
	if n.Timestamp.IsZero() {
		n.Timestamp = time.Now()
	}

	data, err := json.Marshal(n)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}

	for _, conn := range conns {
		if err := conn.Write(ctx, data); err != nil {
			return err
		}
	}
	return nil
}

// Broadcast sends a notification to all online users.
func (h *Hub) Broadcast(ctx context.Context, n core.Notification) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if n.Type == "" {
		n.Type = "notification"
	}
	if n.Timestamp.IsZero() {
		n.Timestamp = time.Now()
	}

	data, _ := json.Marshal(n)
	for uid, userConns := range h.conns {
		for _, conn := range userConns {
			if err := conn.Write(ctx, data); err != nil {
				if h.logger != nil {
					h.logger.Error("Broadcasting notification to %s: %v", uid, err)
				}
			}
		}
	}
}
