package auth

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/Aswanidev-vs/mana/core"
	"github.com/Aswanidev-vs/mana/storage/db"
	"golang.org/x/crypto/bcrypt"
)

type SQLAccountStore struct {
	backend *db.Backend
	prefix  string
}

func NewSQLAccountStore(backend *db.Backend) (*SQLAccountStore, error) {
	return NewSQLAccountStoreWithPrefix(backend, "")
}

func NewSQLAccountStoreWithPrefix(backend *db.Backend, prefix string) (*SQLAccountStore, error) {
	if prefix != "" && !strings.HasSuffix(prefix, "_") {
		prefix += "_"
	}

	// Initialize tables
	err := initializeAccountTables(backend.DB, backend.Driver, prefix)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize account tables: %w", err)
	}

	return &SQLAccountStore{
		backend: backend,
		prefix:  prefix,
	}, nil
}

func initializeAccountTables(dbConn *sql.DB, driver string, prefix string) error {
	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %saccounts (
			user_id TEXT PRIMARY KEY,
			username TEXT UNIQUE,
			password_hash TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`, prefix)

	_, err := dbConn.Exec(query)
	return err
}

func (s *SQLAccountStore) CreateUser(ctx context.Context, username, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	userID := fmt.Sprintf("u-%s", username) // Simple deterministic ID for demo
	query := fmt.Sprintf("INSERT INTO %saccounts (user_id, username, password_hash) VALUES (?, ?, ?)", s.prefix)
	
	if s.backend.Driver == db.Postgres {
		query = fmt.Sprintf("INSERT INTO %saccounts (user_id, username, password_hash) VALUES ($1, $2, $3)", s.prefix)
	}

	_, err = s.conn(ctx).ExecContext(ctx, query, userID, username, string(hash))
	return err
}

func (s *SQLAccountStore) CreateUserWithContact(ctx context.Context, username, password, phone, email string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	userID := fmt.Sprintf("u-%s", username)
	query := fmt.Sprintf("INSERT INTO %saccounts (user_id, username, password_hash, phone, email) VALUES (?, ?, ?, ?, ?)", s.prefix)
	
	if s.backend.Driver == db.Postgres {
		query = fmt.Sprintf("INSERT INTO %saccounts (user_id, username, password_hash, phone, email) VALUES ($1, $2, $3, $4, $5)", s.prefix)
	}

	_, err = s.conn(ctx).ExecContext(ctx, query, userID, username, string(hash), phone, email)
	return err
}

func (s *SQLAccountStore) Authenticate(ctx context.Context, username, password string) (string, error) {
	query := fmt.Sprintf("SELECT user_id, password_hash FROM %saccounts WHERE username = ?", s.prefix)
	if s.backend.Driver == db.Postgres {
		query = fmt.Sprintf("SELECT user_id, password_hash FROM %saccounts WHERE username = $1", s.prefix)
	}

	var userID, hash string
	err := s.conn(ctx).QueryRowContext(ctx, query, username).Scan(&userID, &hash)
	if err != nil {
		return "", err
	}

	err = bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if err != nil {
		return "", fmt.Errorf("invalid password")
	}

	return userID, nil
}

func (s *SQLAccountStore) AuthenticateByPhone(ctx context.Context, phone, password string) (string, error) {
	query := fmt.Sprintf("SELECT user_id, password_hash FROM %saccounts WHERE phone = ?", s.prefix)
	if s.backend.Driver == db.Postgres {
		query = fmt.Sprintf("SELECT user_id, password_hash FROM %saccounts WHERE phone = $1", s.prefix)
	}

	var userID, hash string
	err := s.conn(ctx).QueryRowContext(ctx, query, phone).Scan(&userID, &hash)
	if err != nil {
		return "", err
	}

	err = bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if err != nil {
		return "", fmt.Errorf("invalid password")
	}

	return userID, nil
}

func (s *SQLAccountStore) AuthenticateByEmail(ctx context.Context, email, password string) (string, error) {
	query := fmt.Sprintf("SELECT user_id, password_hash FROM %saccounts WHERE email = ?", s.prefix)
	if s.backend.Driver == db.Postgres {
		query = fmt.Sprintf("SELECT user_id, password_hash FROM %saccounts WHERE email = $1", s.prefix)
	}

	var userID, hash string
	err := s.conn(ctx).QueryRowContext(ctx, query, email).Scan(&userID, &hash)
	if err != nil {
		if err == sql.ErrNoRows {
			fmt.Printf("[SQLAccountStore] No account found for email: %s\n", email)
		} else {
			fmt.Printf("[SQLAccountStore] Email lookup error for %s: %v\n", email, err)
		}
		return "", err
	}

	err = bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if err != nil {
		return "", fmt.Errorf("invalid password")
	}

	return userID, nil
}

func (s *SQLAccountStore) GetUser(ctx context.Context, userID string) (core.User, error) {
	query := fmt.Sprintf("SELECT user_id, username FROM %saccounts WHERE user_id = ?", s.prefix)
	if s.backend.Driver == db.Postgres {
		query = fmt.Sprintf("SELECT user_id, username FROM %saccounts WHERE user_id = $1", s.prefix)
	}

	var user core.User
	err := s.conn(ctx).QueryRowContext(ctx, query, userID).Scan(&user.ID, &user.Username)
	if err != nil {
		return core.User{}, err
	}

	return user, nil
}

func (s *SQLAccountStore) DeleteUser(ctx context.Context, userID string) error {
	query := fmt.Sprintf("DELETE FROM %saccounts WHERE user_id = ?", s.prefix)
	if s.backend.Driver == db.Postgres {
		query = fmt.Sprintf("DELETE FROM %saccounts WHERE user_id = $1", s.prefix)
	}

	_, err := s.conn(ctx).ExecContext(ctx, query, userID)
	return err
}

func (s *SQLAccountStore) conn(ctx context.Context) interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
} {
	if tx := db.GetTx(ctx); tx != nil {
		return tx
	}
	return s.backend
}
