package sqlite

import (
	"database/sql"
	_ "modernc.org/sqlite"
)

// Schema definer for SQLite.
func Initialize(db *sql.DB) error {
	_, err := db.Exec(`
		PRAGMA journal_mode=WAL;
		PRAGMA synchronous=NORMAL;
	`)
	return err
}

// SQL query dialect for SQLite.
const (
	UpsertMessage = `INSERT INTO messages (id, room_id, sender_id, target_id, type, payload, sequence, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET sequence=excluded.sequence, timestamp=excluded.timestamp`
)
