package mysql

import (
	"database/sql"
	_ "github.com/go-sql-driver/mysql"
)

// Initialize MySQL options
func Initialize(db *sql.DB) error {
	// Standard connection and pooling settings are handled in the backend
	return nil
}

// SQL query dialect for MySQL.
const (
	UpsertMessage = `INSERT INTO messages (id, room_id, sender_id, target_id, type, payload, sequence, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE sequence=VALUES(sequence), timestamp=VALUES(timestamp)`
)
