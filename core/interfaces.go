package core

import (
	"context"
	"time"
)

// MessageStore defines the interface for persisting and retrieving messages.
type MessageStore interface {
	SaveMessage(ctx context.Context, msg Message, recipients []string) (Message, error)
	MarkDelivered(ctx context.Context, messageID, userID string) error
	PendingForUser(ctx context.Context, userID string) []Message
	SyncForUserSince(ctx context.Context, userID string, since time.Time) []Message
	SyncForUserAfterSequence(ctx context.Context, userID string, after uint64, limit int) ([]Message, bool)
	LatestSequenceForUser(ctx context.Context, userID string) uint64
}

// AccountStore defines the interface for user authentication and management.
type AccountStore interface {
	CreateUser(ctx context.Context, username, password string) error
	Authenticate(ctx context.Context, username, password string) (userID string, err error)
	GetUser(ctx context.Context, userID string) (User, error)
}

// ProfileStore defines the interface for user profiles.
type ProfileStore interface {
	UpsertProfile(ctx context.Context, profile UserProfile) error
	GetProfile(ctx context.Context, userID string) (UserProfile, error)
}

// UserProfile is a simplified version of the profile for the core interface.
type UserProfile struct {
	UserID      string                 `json:"user_id"`
	DisplayName string                 `json:"display_name"`
	AvatarURL   string                 `json:"avatar_url"`
	Metadata    map[string]interface{} `json:"metadata"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// PreferenceStore defines the interface for user preferences.
type PreferenceStore interface {
	SetPreference(ctx context.Context, userID, key string, value interface{}) error
	GetPreference(ctx context.Context, userID, key string) (interface{}, error)
}

// ContactStore defines the interface for managing contacts and blocking.
type ContactStore interface {
	AddContact(ctx context.Context, userID, contactID string) error
	GetContacts(ctx context.Context, userID string) ([]string, error)
	BlockUser(ctx context.Context, userID, targetID string) error
	IsBlocked(ctx context.Context, userID, targetID string) (bool, error)
}

// DeviceStore defines the interface for managing linked devices.
type DeviceStore interface {
	RegisterDevice(ctx context.Context, userID string, device DeviceInfo) error
	GetDevices(ctx context.Context, userID string) ([]DeviceInfo, error)
	DeleteDevice(ctx context.Context, userID, deviceID string) error
}

// DeviceInfo represents a linked device.
type DeviceInfo struct {
	DeviceID   string    `json:"device_id"`
	Label      string    `json:"label"`
	Platform   string    `json:"platform"`
	LastSeenAt time.Time `json:"last_seen_at"`
}

// StoreProvider provides access to all specialized storage components.
type StoreProvider interface {
	Messages() MessageStore
	Accounts() AccountStore
	Profiles() ProfileStore
	Preferences() PreferenceStore
	Contacts() ContactStore
	Devices() DeviceStore
}
