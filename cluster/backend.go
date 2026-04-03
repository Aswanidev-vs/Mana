package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/Aswanidev-vs/mana/core"
)

const (
	EventDirect = "direct_signal"
	EventRoom   = "room_signal"
)

// Event represents a cluster fanout unit replicated across nodes.
type Event struct {
	Type     string      `json:"type"`
	NodeID   string      `json:"node_id"`
	RoomID   string      `json:"room_id,omitempty"`
	SenderID string      `json:"sender_id,omitempty"`
	Signal   core.Signal `json:"signal"`
}

// Backend publishes and subscribes to cluster events.
type Backend interface {
	Kind() string
	Publish(context.Context, Event) error
	Subscribe(func(Event)) (io.Closer, error)
	Close() error
}

// MemoryBackend provides in-process fanout for tests and local development.
type MemoryBackend struct {
	mu          sync.RWMutex
	subscribers map[int]func(Event)
	nextID      int
}

func NewMemoryBackend() *MemoryBackend {
	return &MemoryBackend{subscribers: make(map[int]func(Event))}
}

func (b *MemoryBackend) Kind() string { return "memory" }

func (b *MemoryBackend) Publish(_ context.Context, event Event) error {
	b.mu.RLock()
	handlers := make([]func(Event), 0, len(b.subscribers))
	for _, handler := range b.subscribers {
		handlers = append(handlers, handler)
	}
	b.mu.RUnlock()

	for _, handler := range handlers {
		go handler(event)
	}
	return nil
}

func (b *MemoryBackend) Subscribe(handler func(Event)) (io.Closer, error) {
	b.mu.Lock()
	id := b.nextID
	b.nextID++
	b.subscribers[id] = handler
	b.mu.Unlock()

	return closerFunc(func() error {
		b.mu.Lock()
		delete(b.subscribers, id)
		b.mu.Unlock()
		return nil
	}), nil
}

func (b *MemoryBackend) Close() error { return nil }

// NewBackendFromConfig creates the configured cluster transport.
func NewBackendFromConfig(cfg core.Config) (Backend, error) {
	switch cfg.PubSubBackend {
	case "", "memory":
		if cfg.PubSubBackend == "" {
			return nil, nil
		}
		return NewMemoryBackend(), nil
	case "redis":
		return NewRedisBackend(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB, cfg.RedisChannel)
	case "nats":
		return NewNATSBackend(cfg.NATSURL, cfg.NATSSubject)
	default:
		return nil, fmt.Errorf("unsupported pubsub backend %q", cfg.PubSubBackend)
	}
}

func encodeEvent(event Event) ([]byte, error) {
	return json.Marshal(event)
}

func decodeEvent(data []byte) (Event, error) {
	var event Event
	err := json.Unmarshal(data, &event)
	return event, err
}

type closerFunc func() error

func (fn closerFunc) Close() error { return fn() }
