package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Aswanidev-vs/mana/storage/db/mysql"
	"github.com/Aswanidev-vs/mana/storage/db/postgres"
	"github.com/Aswanidev-vs/mana/storage/db/sqlite"
)

// Driver types supported by the framework.
const (
	SQLite   = "sqlite"
	Postgres = "postgres"
	MySQL    = "mysql"
)

// Backend handles the shared database connection and pool settings.
type Backend struct {
	*sql.DB
	Driver string
	DSN    string
}

// NewBackendFromDB wraps an existing *sql.DB connection into a Mana Backend.
func NewBackendFromDB(db *sql.DB, driver string) (*Backend, error) {
	if db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}
	backend := &Backend{
		DB:     db,
		Driver: strings.ToLower(driver),
	}
	if err := backend.initialize(); err != nil {
		return nil, err
	}
	return backend, nil
}

// NewBackend creates and initializes a new database backend based on the config.
func NewBackend(driver, dsn string) (*Backend, error) {
	if driver == "" {
		return nil, fmt.Errorf("database driver is required")
	}

	// Normalizing driver names
	driver = strings.ToLower(driver)

	var sqlDriver string
	switch driver {
	case SQLite:
		sqlDriver = "sqlite"
	case Postgres:
		sqlDriver = "pgx"
	case MySQL:
		sqlDriver = "mysql"
	default:
		return nil, fmt.Errorf("unsupported database driver: %s", driver)
	}

	db, err := sql.Open(sqlDriver, dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Standard connection pool settings (can be made configurable later)
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Verify connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	backend := &Backend{
		DB:     db,
		Driver: driver,
		DSN:    dsn,
	}

	// Initialize driver-specific settings
	if err := backend.initialize(); err != nil {
		return nil, err
	}

	return backend, nil
}

func (b *Backend) initialize() error {
	switch b.Driver {
	case SQLite:
		return sqlite.Initialize(b.DB)
	case Postgres:
		return postgres.Initialize(b.DB)
	case MySQL:
		return mysql.Initialize(b.DB)
	}
	return nil
}

// --- Transaction Helpers ---

type txKey struct{}

// WithTx attaches a transaction to a context.
func WithTx(ctx context.Context, tx *sql.Tx) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}

// GetTx retrieves a transaction from a context, if present.
func GetTx(ctx context.Context) *sql.Tx {
	if tx, ok := ctx.Value(txKey{}).(*sql.Tx); ok {
		return tx
	}
	return nil
}
// RunInTx executes a function within a transaction.
// If the context already contains a transaction, it uses that one.
// Otherwise, it starts a new transaction and commits/rolls back as needed.
func (b *Backend) RunInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if tx := GetTx(ctx); tx != nil {
		return fn(ctx)
	}

	tx, err := b.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(WithTx(ctx, tx)); err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}
