package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/Aswanidev-vs/mana/core"
)

type DeliveryState string

const (
	DeliveryPending   DeliveryState = "pending"
	DeliveryDelivered DeliveryState = "delivered"
	DeliveryRead      DeliveryState = "read"
)

type StoredMessage struct {
	Message    core.Message             `json:"message"`
	Recipients []string                 `json:"recipients"`
	Delivery   map[string]DeliveryState `json:"delivery"`
}

type snapshot struct {
	Messages []StoredMessage `json:"messages"`
}

type MessageStore struct {
	mu       sync.RWMutex
	path     string
	messages []StoredMessage
}

func NewMessageStore(path string) (*MessageStore, error) {
	store := &MessageStore{
		path:     path,
		messages: make([]StoredMessage, 0),
	}
	if path == "" {
		return store, nil
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *MessageStore) SaveMessage(msg core.Message, recipients []string) (core.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if msg.ID == "" {
		msg.ID = fmt.Sprintf("msg-%d", time.Now().UnixNano())
	}
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}

	recipients = uniqueStrings(recipients)
	delivery := make(map[string]DeliveryState, len(recipients))
	for _, recipient := range recipients {
		delivery[recipient] = DeliveryPending
	}

	s.messages = append(s.messages, StoredMessage{
		Message:    msg,
		Recipients: recipients,
		Delivery:   delivery,
	})

	return msg, s.persistLocked()
}

func (s *MessageStore) MarkDelivered(messageID, userID string) error {
	return s.updateDelivery(messageID, userID, DeliveryDelivered)
}

func (s *MessageStore) PendingForUser(userID string) []core.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pending := make([]core.Message, 0)
	for _, item := range s.messages {
		if state, ok := item.Delivery[userID]; ok && state == DeliveryPending {
			pending = append(pending, item.Message)
		}
	}
	return pending
}

func (s *MessageStore) SyncForUserSince(userID string, since time.Time) []core.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]core.Message, 0)
	for _, item := range s.messages {
		if !item.Message.Timestamp.After(since) {
			continue
		}
		if item.Message.SenderID == userID || slices.Contains(item.Recipients, userID) {
			result = append(result, item.Message)
		}
	}
	return result
}

func (s *MessageStore) updateDelivery(messageID, userID string, state DeliveryState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.messages {
		if s.messages[i].Message.ID != messageID {
			continue
		}
		if s.messages[i].Delivery == nil {
			s.messages[i].Delivery = make(map[string]DeliveryState)
		}
		if s.messages[i].Delivery[userID] == DeliveryRead {
			return nil
		}
		s.messages[i].Delivery[userID] = state
		return s.persistLocked()
	}
	return nil
}

func (s *MessageStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var snapshot snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("load message store: %w", err)
	}
	s.messages = snapshot.Messages
	return nil
}

func (s *MessageStore) persistLocked() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(snapshot{Messages: s.messages}, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.path)
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
