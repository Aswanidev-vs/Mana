package e2ee

import (
	"time"
)

// Session tracks the E2EE state between two users/devices.
type Session struct {
	ID        string    `json:"id"`
	UserA     string    `json:"user_a"`
	UserB     string    `json:"user_b"`
	State     []byte    `json:"state"` // Serialized Double Ratchet state
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// DefaultSessionManager manages E2EE session persistence.
type DefaultSessionManager struct {
	store KeyStore
}

// NewSessionManager creates a new session manager backed by a KeyStore.
func NewSessionManager(store KeyStore) *DefaultSessionManager {
	return &DefaultSessionManager{store: store}
}

// CreateSessionID returns a deterministic session ID for a pair of user:device identifiers.
func CreateSessionID(userA, deviceA, userB, deviceB string) string {
	idA := userA + ":" + deviceA
	idB := userB + ":" + deviceB
	if idA < idB {
		return idA + "::" + idB
	}
	return idB + "::" + idA
}
