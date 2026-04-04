package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Aswanidev-vs/mana/storage/db"
)

type SQLPreferenceStore struct {
	backend *db.Backend
	prefix  string
}

func NewSQLPreferenceStore(backend *db.Backend) (*SQLPreferenceStore, error) {
	return NewSQLPreferenceStoreWithPrefix(backend, "")
}

func NewSQLPreferenceStoreWithPrefix(backend *db.Backend, prefix string) (*SQLPreferenceStore, error) {
	if prefix != "" && !strings.HasSuffix(prefix, "_") {
		prefix += "_"
	}

	// Initialize tables
	err := initializeSettingsTables(backend.DB, backend.Driver, prefix)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize settings tables: %w", err)
	}

	return &SQLPreferenceStore{
		backend: backend,
		prefix:  prefix,
	}, nil
}

func initializeSettingsTables(dbConn *sql.DB, driver string, prefix string) error {
	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %spreferences (
			user_id TEXT,
			key TEXT,
			value BLOB,
			PRIMARY KEY (user_id, key)
		)
	`, prefix)

	_, err := dbConn.Exec(query)
	return err
}

func (s *SQLPreferenceStore) SetPreference(ctx context.Context, userID, key string, value interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}

	query := fmt.Sprintf(`
		INSERT INTO %spreferences (user_id, key, value)
		VALUES (?, ?, ?)
		ON CONFLICT(user_id, key) DO UPDATE SET value = EXCLUDED.value
	`, s.prefix)

	if s.backend.Driver == db.Postgres {
		query = fmt.Sprintf(`
			INSERT INTO %spreferences (user_id, key, value)
			VALUES ($1, $2, $3)
			ON CONFLICT(user_id, key) DO UPDATE SET value = EXCLUDED.value
		`, s.prefix)
	} else if s.backend.Driver == db.MySQL {
		query = fmt.Sprintf(`
			INSERT INTO %spreferences (user_id, key, value)
			VALUES (?, ?, ?)
			ON DUPLICATE KEY UPDATE value = VALUES(value)
		`, s.prefix)
	}

	_, err = s.conn(ctx).ExecContext(ctx, query, userID, key, data)
	return err
}

func (s *SQLPreferenceStore) GetPreference(ctx context.Context, userID, key string) (interface{}, error) {
	query := fmt.Sprintf("SELECT value FROM %spreferences WHERE user_id = ? AND key = ?", s.prefix)
	if s.backend.Driver == db.Postgres {
		query = fmt.Sprintf("SELECT value FROM %spreferences WHERE user_id = $1 AND key = $2", s.prefix)
	}

	var data []byte
	err := s.conn(ctx).QueryRowContext(ctx, query, userID, key).Scan(&data)
	if err != nil {
		return nil, err
	}

	var val interface{}
	if err := json.Unmarshal(data, &val); err != nil {
		return nil, err
	}
	return val, nil
}

func (s *SQLPreferenceStore) conn(ctx context.Context) interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
} {
	if tx := db.GetTx(ctx); tx != nil {
		return tx
	}
	return s.backend
}
