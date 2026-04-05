package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Aswanidev-vs/mana/core"
	"github.com/Aswanidev-vs/mana/storage/db"
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

type JSONMessageStore struct {
	mu       sync.RWMutex
	path     string
	messages []StoredMessage
	nextSeq  uint64
}

// NewMessageStore creates a new message store.
func NewMessageStore(path string) (core.MessageStore, error) {
	if strings.HasSuffix(path, ".db") || strings.HasSuffix(path, ".sqlite") {
		backend, err := db.NewBackend(db.SQLite, path)
		if err != nil {
			return nil, err
		}
		return NewSQLMessageStore(backend)
	}

	store := &JSONMessageStore{
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

func (s *JSONMessageStore) SaveMessage(ctx context.Context, msg core.Message, recipients []string) (core.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if msg.ID == "" {
		msg.ID = fmt.Sprintf("msg-%d", time.Now().UnixNano())
	}
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}
	if msg.Sequence == 0 {
		s.nextSeq++
		msg.Sequence = s.nextSeq
	} else if msg.Sequence > s.nextSeq {
		s.nextSeq = msg.Sequence
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

func (s *JSONMessageStore) MarkDelivered(ctx context.Context, messageID, userID string) error {
	return s.updateDelivery(messageID, userID, DeliveryDelivered)
}

func (s *JSONMessageStore) PendingForUser(ctx context.Context, userID string) []core.Message {
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

func (s *JSONMessageStore) SyncForUserSince(ctx context.Context, userID string, since time.Time) []core.Message {
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

func (s *JSONMessageStore) SyncForUserAfterSequence(ctx context.Context, userID string, after uint64, limit int) ([]core.Message, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = len(s.messages)
	}

	result := make([]core.Message, 0, min(limit, len(s.messages)))
	hasMore := false
	for _, item := range s.messages {
		if item.Message.Sequence <= after {
			continue
		}
		if item.Message.SenderID != userID && !slices.Contains(item.Recipients, userID) {
			continue
		}
		if len(result) >= limit {
			hasMore = true
			break
		}
		result = append(result, item.Message)
	}
	return result, hasMore
}

func (s *JSONMessageStore) LatestSequenceForUser(ctx context.Context, userID string) uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var latest uint64
	for _, item := range s.messages {
		if item.Message.SenderID != userID && !slices.Contains(item.Recipients, userID) {
			continue
		}
		if item.Message.Sequence > latest {
			latest = item.Message.Sequence
		}
	}
	return latest
}

func (s *JSONMessageStore) GetConversation(ctx context.Context, userID, contactID string, limit int) ([]core.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 50
	}

	result := make([]core.Message, 0)
	// Iterate backwards to get most recent messages
	count := 0
	for i := len(s.messages) - 1; i >= 0 && count < limit; i-- {
		m := s.messages[i].Message
		isMe := m.SenderID == userID && (m.TargetID == contactID || slices.Contains(s.messages[i].Recipients, contactID))
		isThem := m.SenderID == contactID && (m.TargetID == userID || slices.Contains(s.messages[i].Recipients, userID))

		if isMe || isThem {
			result = append(result, m)
			count++
		}
	}

	// Reverse to bring back to chronological order
	slices.Reverse(result)
	return result, nil
}

func (s *JSONMessageStore) updateDelivery(messageID, userID string, state DeliveryState) error {
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

func (s *JSONMessageStore) load() error {
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
	for _, item := range s.messages {
		if item.Message.Sequence > s.nextSeq {
			s.nextSeq = item.Message.Sequence
		}
	}
	return nil
}

func (s *JSONMessageStore) persistLocked() error {
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
