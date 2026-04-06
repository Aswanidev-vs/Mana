package e2ee

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Aswanidev-vs/mana/storage/db"
)

// SQLKeyStore implements the KeyStore interface using a SQL database.
// The server uses this to store public identity keys and prekey bundles.
// Clients use this to store session state.
type SQLKeyStore struct {
	backend *db.Backend
	prefix  string
}

// NewSQLKeyStore creates a new SQL-based key store.
func NewSQLKeyStore(backend *db.Backend, prefix string) (*SQLKeyStore, error) {
	if prefix != "" && !strings.HasSuffix(prefix, "_") {
		prefix += "_"
	}

	s := &SQLKeyStore{
		backend: backend,
		prefix:  prefix,
	}

	if err := s.initializeTables(); err != nil {
		return nil, fmt.Errorf("failed to initialize E2EE tables: %w", err)
	}

	return s, nil
}

func (s *SQLKeyStore) initializeTables() error {
	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %se2ee_identities (
			user_id TEXT PRIMARY KEY,
			identity_key BLOB NOT NULL
		);
		CREATE TABLE IF NOT EXISTS %se2ee_sessions (
			session_id TEXT PRIMARY KEY,
			state BLOB NOT NULL
		);
		CREATE TABLE IF NOT EXISTS %se2ee_prekeys (
			user_id TEXT PRIMARY KEY,
			bundle BLOB NOT NULL
		);
	`, s.prefix, s.prefix, s.prefix)

	_, err := s.backend.Exec(query)
	return err
}

// --- Identity Public Keys ---

func (s *SQLKeyStore) SaveIdentityPublicKey(ctx context.Context, userID string, pubKey []byte) error {
	query := fmt.Sprintf("INSERT INTO %se2ee_identities (user_id, identity_key) VALUES (?, ?) ON CONFLICT(user_id) DO UPDATE SET identity_key=excluded.identity_key", s.prefix)
	if s.backend.Driver == db.Postgres {
		query = fmt.Sprintf("INSERT INTO %se2ee_identities (user_id, identity_key) VALUES ($1, $2) ON CONFLICT(user_id) DO UPDATE SET identity_key=EXCLUDED.identity_key", s.prefix)
	}
	_, err := s.conn(ctx).ExecContext(ctx, query, userID, pubKey)
	return err
}

func (s *SQLKeyStore) LoadIdentityPublicKey(ctx context.Context, userID string) ([]byte, error) {
	var key []byte
	query := fmt.Sprintf("SELECT identity_key FROM %se2ee_identities WHERE user_id = ?", s.prefix)
	if s.backend.Driver == db.Postgres {
		query = fmt.Sprintf("SELECT identity_key FROM %se2ee_identities WHERE user_id = $1", s.prefix)
	}
	err := s.conn(ctx).QueryRowContext(ctx, query, userID).Scan(&key)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return key, err
}

// --- PreKey Bundles ---

func (s *SQLKeyStore) SavePreKeyBundle(ctx context.Context, userID string, bundle *PublicPreKeyBundle) error {
	bundle.MarshalKeys()
	jsonData, err := json.Marshal(bundle)
	if err != nil {
		return fmt.Errorf("marshal prekey bundle: %w", err)
	}

	// Layer Base64 and Versioning to prevent driver-level character corruption
	b64Data := base64.StdEncoding.EncodeToString(jsonData)
	versionedData := []byte("v1:" + b64Data)

	query := fmt.Sprintf("INSERT INTO %se2ee_prekeys (user_id, bundle) VALUES (?, ?) ON CONFLICT(user_id) DO UPDATE SET bundle=excluded.bundle", s.prefix)
	if s.backend.Driver == db.Postgres {
		query = fmt.Sprintf("INSERT INTO %se2ee_prekeys (user_id, bundle) VALUES ($1, $2) ON CONFLICT(user_id) DO UPDATE SET bundle=EXCLUDED.bundle", s.prefix)
	}
	_, err = s.conn(ctx).ExecContext(ctx, query, userID, versionedData)
	return err
}

func (s *SQLKeyStore) LoadPreKeyBundle(ctx context.Context, userID string) (*PublicPreKeyBundle, error) {
	var data []byte
	query := fmt.Sprintf("SELECT bundle FROM %se2ee_prekeys WHERE user_id = ?", s.prefix)
	if s.backend.Driver == db.Postgres {
		query = fmt.Sprintf("SELECT bundle FROM %se2ee_prekeys WHERE user_id = $1", s.prefix)
	}
	err := s.conn(ctx).QueryRowContext(ctx, query, userID).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var jsonData []byte
	versionPrefix := []byte("v1:")

	// Detect storage version using zero-copy prefix check
	if bytes.HasPrefix(data, versionPrefix) {
		// Version 1: Base64 Encoded JSON
		b64Content := data[len(versionPrefix):]
		decoded, err := base64.StdEncoding.DecodeString(string(b64Content))
		if err != nil {
			return nil, fmt.Errorf("failed to decode v1 prekey bundle: %w", err)
		}
		jsonData = decoded
	} else if len(data) > 0 && data[0] == '{' {
		// Legacy: Raw JSON
		jsonData = data
	} else {
		return nil, fmt.Errorf("unknown prekey bundle format for user %s", userID)
	}

	var bundle PublicPreKeyBundle
	if err := json.Unmarshal(jsonData, &bundle); err != nil {
		return nil, fmt.Errorf("failed to unmarshal bundle from %s: %w", userID, err)
	}
	if err := bundle.UnmarshalKeys(); err != nil {
		return nil, fmt.Errorf("unmarshal prekey bundle keys: %w", err)
	}
	return &bundle, nil
}

func (s *SQLKeyStore) ConsumeOneTimePreKey(ctx context.Context, userID string) error {
	return s.backend.RunInTx(ctx, func(ctx context.Context) error {
		// Load the bundle, clear one OPK, and save it back
		bundle, err := s.LoadPreKeyBundle(ctx, userID)
		if err != nil || bundle == nil {
			return err
		}
		if len(bundle.OneTimePreKeys) == 0 {
			return nil // No OPKs to consume
		}

		// Pop the first key (smallest ID)
		var firstID uint32
		var found bool
		for id := range bundle.OneTimePreKeys {
			if !found || id < firstID {
				firstID = id
				found = true
			}
		}

		if found {
			delete(bundle.OneTimePreKeys, firstID)
			delete(bundle.OneTimePreKeysBytes, firstID)
		}

		return s.SavePreKeyBundle(ctx, userID, bundle)
	})
}

// --- Session State ---

func (s *SQLKeyStore) SaveSession(ctx context.Context, sessionID string, state []byte) error {
	query := fmt.Sprintf("INSERT INTO %se2ee_sessions (session_id, state) VALUES (?, ?) ON CONFLICT(session_id) DO UPDATE SET state=excluded.state", s.prefix)
	if s.backend.Driver == db.Postgres {
		query = fmt.Sprintf("INSERT INTO %se2ee_sessions (session_id, state) VALUES ($1, $2) ON CONFLICT(session_id) DO UPDATE SET state=EXCLUDED.state", s.prefix)
	}
	_, err := s.conn(ctx).ExecContext(ctx, query, sessionID, state)
	return err
}

func (s *SQLKeyStore) LoadSession(ctx context.Context, sessionID string) ([]byte, error) {
	var state []byte
	query := fmt.Sprintf("SELECT state FROM %se2ee_sessions WHERE session_id = ?", s.prefix)
	if s.backend.Driver == db.Postgres {
		query = fmt.Sprintf("SELECT state FROM %se2ee_sessions WHERE session_id = $1", s.prefix)
	}
	err := s.conn(ctx).QueryRowContext(ctx, query, sessionID).Scan(&state)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return state, err
}

// conn returns either the transaction in the context or the base backend.
func (s *SQLKeyStore) conn(ctx context.Context) interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
} {
	if tx := db.GetTx(ctx); tx != nil {
		return tx
	}
	return s.backend
}
