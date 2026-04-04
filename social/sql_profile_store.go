package social

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Aswanidev-vs/mana/core"
	"github.com/Aswanidev-vs/mana/storage/db"
)

type SQLSocialStore struct {
	backend *db.Backend
	prefix  string
}

func NewSQLSocialStore(backend *db.Backend) (*SQLSocialStore, error) {
	return NewSQLSocialStoreWithPrefix(backend, "")
}

func NewSQLSocialStoreWithPrefix(backend *db.Backend, prefix string) (*SQLSocialStore, error) {
	if prefix != "" && !strings.HasSuffix(prefix, "_") {
		prefix += "_"
	}

	// Initialize tables
	err := initializeSocialTables(backend.DB, backend.Driver, prefix)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize social tables: %w", err)
	}

	return &SQLSocialStore{
		backend: backend,
		prefix:  prefix,
	}, nil
}

func initializeSocialTables(dbConn *sql.DB, driver string, prefix string) error {
	queries := []string{
		fmt.Sprintf(`
			CREATE TABLE IF NOT EXISTS %sprofiles (
				user_id TEXT PRIMARY KEY,
				display_name TEXT,
				avatar_url TEXT,
				metadata BLOB,
				updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
			)
		`, prefix),
		fmt.Sprintf(`
			CREATE TABLE IF NOT EXISTS %scontacts (
				user_id TEXT,
				contact_id TEXT,
				status TEXT,
				updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				PRIMARY KEY (user_id, contact_id)
			)
		`, prefix),
		fmt.Sprintf(`
			CREATE TABLE IF NOT EXISTS %sblocks (
				user_id TEXT,
				target_id TEXT,
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				PRIMARY KEY (user_id, target_id)
			)
		`, prefix),
	}

	for _, q := range queries {
		if _, err := dbConn.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

// ProfileStore implementation

func (s *SQLSocialStore) UpsertProfile(ctx context.Context, profile core.UserProfile) error {
	query := fmt.Sprintf(`
		INSERT INTO %sprofiles (user_id, display_name, avatar_url, metadata, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			display_name = EXCLUDED.display_name,
			avatar_url = EXCLUDED.avatar_url,
			metadata = EXCLUDED.metadata,
			updated_at = EXCLUDED.updated_at
	`, s.prefix)

	if s.backend.Driver == db.Postgres {
		query = fmt.Sprintf(`
			INSERT INTO %sprofiles (user_id, display_name, avatar_url, metadata, updated_at)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT(user_id) DO UPDATE SET
				display_name = EXCLUDED.display_name,
				avatar_url = EXCLUDED.avatar_url,
				metadata = EXCLUDED.metadata,
				updated_at = EXCLUDED.updated_at
		`, s.prefix)
	} else if s.backend.Driver == db.MySQL {
		query = fmt.Sprintf(`
			INSERT INTO %sprofiles (user_id, display_name, avatar_url, metadata, updated_at)
			VALUES (?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE
				display_name = VALUES(display_name),
				avatar_url = VALUES(avatar_url),
				metadata = VALUES(metadata),
				updated_at = VALUES(updated_at)
		`, s.prefix)
	}

	// For demo purpose, we assume metadata is already serialized or we serialize it here
	_, err := s.conn(ctx).ExecContext(ctx, query, profile.UserID, profile.DisplayName, profile.AvatarURL, "{}", time.Now())
	return err
}

func (s *SQLSocialStore) GetProfile(ctx context.Context, userID string) (core.UserProfile, error) {
	query := fmt.Sprintf("SELECT user_id, display_name, avatar_url, updated_at FROM %sprofiles WHERE user_id = ?", s.prefix)
	if s.backend.Driver == db.Postgres {
		query = fmt.Sprintf("SELECT user_id, display_name, avatar_url, updated_at FROM %sprofiles WHERE user_id = $1", s.prefix)
	}

	var p core.UserProfile
	err := s.conn(ctx).QueryRowContext(ctx, query, userID).Scan(&p.UserID, &p.DisplayName, &p.AvatarURL, &p.UpdatedAt)
	if err != nil {
		return core.UserProfile{}, err
	}
	return p, nil
}

// ContactStore implementation

func (s *SQLSocialStore) AddContact(ctx context.Context, userID, contactID string) error {
	query := fmt.Sprintf("INSERT INTO %scontacts (user_id, contact_id, status, updated_at) VALUES (?, ?, ?, ?)", s.prefix)
	if s.backend.Driver == db.Postgres {
		query = fmt.Sprintf("INSERT INTO %scontacts (user_id, contact_id, status, updated_at) VALUES ($1, $2, $3, $4)", s.prefix)
	}
	_, err := s.conn(ctx).ExecContext(ctx, query, userID, contactID, "accepted", time.Now())
	return err
}

func (s *SQLSocialStore) GetContacts(ctx context.Context, userID string) ([]string, error) {
	query := fmt.Sprintf("SELECT contact_id FROM %scontacts WHERE user_id = ?", s.prefix)
	if s.backend.Driver == db.Postgres {
		query = fmt.Sprintf("SELECT contact_id FROM %scontacts WHERE user_id = $1", s.prefix)
	}
	rows, err := s.conn(ctx).QueryContext(ctx, query, userID)
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

func (s *SQLSocialStore) BlockUser(ctx context.Context, userID, targetID string) error {
	query := fmt.Sprintf("INSERT INTO %sblocks (user_id, target_id) VALUES (?, ?)", s.prefix)
	if s.backend.Driver == db.Postgres {
		query = fmt.Sprintf("INSERT INTO %sblocks (user_id, target_id) VALUES ($1, $2)", s.prefix)
	}
	_, err := s.conn(ctx).ExecContext(ctx, query, userID, targetID)
	return err
}

func (s *SQLSocialStore) IsBlocked(ctx context.Context, userID, targetID string) (bool, error) {
	query := fmt.Sprintf("SELECT COUNT(1) FROM %sblocks WHERE user_id = ? AND target_id = ?", s.prefix)
	if s.backend.Driver == db.Postgres {
		query = fmt.Sprintf("SELECT COUNT(1) FROM %sblocks WHERE user_id = $1 AND target_id = $2", s.prefix)
	}
	var count int
	err := s.conn(ctx).QueryRowContext(ctx, query, userID, targetID).Scan(&count)
	return count > 0, err
}

func (s *SQLSocialStore) conn(ctx context.Context) interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
} {
	if tx := db.GetTx(ctx); tx != nil {
		return tx
	}
	return s.backend
}
