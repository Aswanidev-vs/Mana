package postgres

import (
	"database/sql"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// Initialize PostgreSQL options
func Initialize(db *sql.DB) error {
	// pgx specific tuning can go here if needed.
	return nil
}

// SQL query dialect for PostgreSQL.
const (
	UpsertMessage = `INSERT INTO messages (id, room_id, sender_id, target_id, type, payload, sequence, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT(id) DO UPDATE SET sequence=EXCLUDED.sequence, timestamp=EXCLUDED.timestamp`
)
