package product

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Aswanidev-vs/mana/core"
	"github.com/Aswanidev-vs/mana/storage/db"
)

type SQLProductStore struct {
	backend *db.Backend
}

func NewSQLProductStore(backend *db.Backend) (*SQLProductStore, error) {
	// Initialize tables for product-related data
	_, err := backend.Exec(`
		CREATE TABLE IF NOT EXISTS profiles (
			user_id TEXT PRIMARY KEY,
			display_name TEXT,
			avatar_url TEXT,
			metadata TEXT,
			updated_at DATETIME
		);
		CREATE TABLE IF NOT EXISTS contacts (
			user_id TEXT,
			contact_id TEXT,
			PRIMARY KEY (user_id, contact_id)
		);
		CREATE TABLE IF NOT EXISTS blocked (
			user_id TEXT,
			target_id TEXT,
			PRIMARY KEY (user_id, target_id)
		);
		CREATE TABLE IF NOT EXISTS devices (
			user_id TEXT,
			device_id TEXT,
			label TEXT,
			platform TEXT,
			last_seen_at DATETIME,
			PRIMARY KEY (user_id, device_id)
		);
		CREATE TABLE IF NOT EXISTS preferences (
			user_id TEXT,
			key TEXT,
			value TEXT,
			PRIMARY KEY (user_id, key)
		);
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to create product tables: %w", err)
	}

	return &SQLProductStore{backend: backend}, nil
}

// ProfileStore Implementation
func (s *SQLProductStore) UpsertProfile(ctx context.Context, profile core.UserProfile) error {
	meta, _ := json.Marshal(profile.Metadata)
	query := `INSERT INTO profiles (user_id, display_name, avatar_url, metadata, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET display_name=excluded.display_name, avatar_url=excluded.avatar_url, metadata=excluded.metadata, updated_at=excluded.updated_at`

	if s.backend.Driver == db.Postgres {
		query = `INSERT INTO profiles (user_id, display_name, avatar_url, metadata, updated_at)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT(user_id) DO UPDATE SET display_name=EXCLUDED.display_name, avatar_url=EXCLUDED.avatar_url, metadata=EXCLUDED.metadata, updated_at=EXCLUDED.updated_at`
	} else if s.backend.Driver == db.MySQL {
		query = `INSERT INTO profiles (user_id, display_name, avatar_url, metadata, updated_at)
			VALUES (?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE display_name=VALUES(display_name), avatar_url=VALUES(avatar_url), metadata=VALUES(metadata), updated_at=VALUES(updated_at)`
	}

	_, err := s.backend.ExecContext(ctx, query, profile.UserID, profile.DisplayName, profile.AvatarURL, string(meta), profile.UpdatedAt)
	return err
}

func (s *SQLProductStore) GetProfile(ctx context.Context, userID string) (core.UserProfile, error) {
	query := "SELECT user_id, display_name, avatar_url, metadata, updated_at FROM profiles WHERE user_id = ?"
	if s.backend.Driver == db.Postgres {
		query = "SELECT user_id, display_name, avatar_url, metadata, updated_at FROM profiles WHERE user_id = $1"
	}

	var p core.UserProfile
	var metaStr string
	err := s.backend.QueryRowContext(ctx, query, userID).Scan(&p.UserID, &p.DisplayName, &p.AvatarURL, &metaStr, &p.UpdatedAt)
	if err != nil {
		return core.UserProfile{}, err
	}
	_ = json.Unmarshal([]byte(metaStr), &p.Metadata)
	return p, nil
}

// ContactStore Implementation
func (s *SQLProductStore) AddContact(ctx context.Context, userID, contactID string) error {
	query := "INSERT INTO contacts (user_id, contact_id) VALUES (?, ?)"
	if s.backend.Driver == db.Postgres {
		query = "INSERT INTO contacts (user_id, contact_id) VALUES ($1, $2) ON CONFLICT DO NOTHING"
	} else if s.backend.Driver == db.SQLite {
		query = "INSERT OR IGNORE INTO contacts (user_id, contact_id) VALUES (?, ?)"
	} else {
		query = "INSERT IGNORE INTO contacts (user_id, contact_id) VALUES (?, ?)"
	}

	_, err := s.backend.ExecContext(ctx, query, userID, contactID)
	return err
}

func (s *SQLProductStore) GetContacts(ctx context.Context, userID string) ([]string, error) {
	query := "SELECT contact_id FROM contacts WHERE user_id = ?"
	if s.backend.Driver == db.Postgres {
		query = "SELECT contact_id FROM contacts WHERE user_id = $1"
	}

	rows, err := s.backend.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var contacts []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err == nil {
			contacts = append(contacts, c)
		}
	}
	return contacts, nil
}

func (s *SQLProductStore) BlockUser(ctx context.Context, userID, targetID string) error {
	query := "INSERT INTO blocked (user_id, target_id) VALUES (?, ?)"
	if s.backend.Driver == db.Postgres {
		query = "INSERT INTO blocked (user_id, target_id) VALUES ($1, $2) ON CONFLICT DO NOTHING"
	} else if s.backend.Driver == db.SQLite {
		query = "INSERT OR IGNORE INTO blocked (user_id, target_id) VALUES (?, ?)"
	} else {
		query = "INSERT IGNORE INTO blocked (user_id, target_id) VALUES (?, ?)"
	}
	_, err := s.backend.ExecContext(ctx, query, userID, targetID)
	return err
}

func (s *SQLProductStore) IsBlocked(ctx context.Context, userID, targetID string) (bool, error) {
	query := "SELECT EXISTS(SELECT 1 FROM blocked WHERE user_id = ? AND target_id = ?)"
	if s.backend.Driver == db.Postgres {
		query = "SELECT EXISTS(SELECT 1 FROM blocked WHERE user_id = $1 AND target_id = $2)"
	}
	var blocked bool
	err := s.backend.QueryRowContext(ctx, query, userID, targetID).Scan(&blocked)
	return blocked, err
}

// DeviceStore Implementation
func (s *SQLProductStore) RegisterDevice(ctx context.Context, userID string, device core.DeviceInfo) error {
	query := `INSERT INTO devices (user_id, device_id, label, platform, last_seen_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id, device_id) DO UPDATE SET label=excluded.label, platform=excluded.platform, last_seen_at=excluded.last_seen_at`

	if s.backend.Driver == db.Postgres {
		query = `INSERT INTO devices (user_id, device_id, label, platform, last_seen_at)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT(user_id, device_id) DO UPDATE SET label=EXCLUDED.label, platform=EXCLUDED.platform, last_seen_at=EXCLUDED.last_seen_at`
	} else if s.backend.Driver == db.MySQL {
		query = `INSERT INTO devices (user_id, device_id, label, platform, last_seen_at)
			VALUES (?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE label=VALUES(label), platform=VALUES(platform), last_seen_at=VALUES(last_seen_at)`
	}

	_, err := s.backend.ExecContext(ctx, query, userID, device.DeviceID, device.Label, device.Platform, device.LastSeenAt)
	return err
}

func (s *SQLProductStore) GetDevices(ctx context.Context, userID string) ([]core.DeviceInfo, error) {
	query := "SELECT device_id, label, platform, last_seen_at FROM devices WHERE user_id = ?"
	if s.backend.Driver == db.Postgres {
		query = "SELECT device_id, label, platform, last_seen_at FROM devices WHERE user_id = $1"
	}

	rows, err := s.backend.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []core.DeviceInfo
	for rows.Next() {
		var d core.DeviceInfo
		if err := rows.Scan(&d.DeviceID, &d.Label, &d.Platform, &d.LastSeenAt); err == nil {
			devices = append(devices, d)
		}
	}
	return devices, nil
}

func (s *SQLProductStore) DeleteDevice(ctx context.Context, userID, deviceID string) error {
	query := "DELETE FROM devices WHERE user_id = ? AND device_id = ?"
	if s.backend.Driver == db.Postgres {
		query = "DELETE FROM devices WHERE user_id = $1 AND device_id = $2"
	}
	_, err := s.backend.ExecContext(ctx, query, userID, deviceID)
	return err
}

// PreferenceStore Implementation
func (s *SQLProductStore) SetPreference(ctx context.Context, userID, key string, value interface{}) error {
	val, _ := json.Marshal(value)
	query := `INSERT INTO preferences (user_id, key, value)
		VALUES (?, ?, ?)
		ON CONFLICT(user_id, key) DO UPDATE SET value=excluded.value`

	if s.backend.Driver == db.Postgres {
		query = `INSERT INTO preferences (user_id, key, value)
			VALUES ($1, $2, $3)
			ON CONFLICT(user_id, key) DO UPDATE SET value=EXCLUDED.value`
	} else if s.backend.Driver == db.MySQL {
		query = `INSERT INTO preferences (user_id, key, value)
			VALUES (?, ?, ?)
			ON DUPLICATE KEY UPDATE value=VALUES(value)`
	}

	_, err := s.backend.ExecContext(ctx, query, userID, key, string(val))
	return err
}

func (s *SQLProductStore) GetPreference(ctx context.Context, userID, key string) (interface{}, error) {
	query := "SELECT value FROM preferences WHERE user_id = ? AND key = ?"
	if s.backend.Driver == db.Postgres {
		query = "SELECT value FROM preferences WHERE user_id = $1 AND key = $2"
	}

	var valStr string
	err := s.backend.QueryRowContext(ctx, query, userID, key).Scan(&valStr)
	if err != nil {
		return nil, err
	}
	var val interface{}
	_ = json.Unmarshal([]byte(valStr), &val)
	return val, nil
}
