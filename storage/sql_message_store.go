package storage

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Aswanidev-vs/mana/core"
	"github.com/Aswanidev-vs/mana/storage/db"
	"github.com/Aswanidev-vs/mana/storage/db/mysql"
	"github.com/Aswanidev-vs/mana/storage/db/postgres"
	"github.com/Aswanidev-vs/mana/storage/db/sqlite"
)

type SQLMessageStore struct {
	backend *db.Backend
	prefix  string
	nextSeq uint64
}

// NewSQLMessageStore creates a new SQL-based message store.
// If prefix is provided, it will be added to all table names (e.g. "mana_messages").
func NewSQLMessageStore(backend *db.Backend) (*SQLMessageStore, error) {
	return NewSQLMessageStoreWithPrefix(backend, "")
}

func NewSQLMessageStoreWithPrefix(backend *db.Backend, prefix string) (*SQLMessageStore, error) {
	if prefix != "" && !strings.HasSuffix(prefix, "_") {
		prefix += "_"
	}

	// Initialize tables
	err := initializeTables(backend.DB, backend.Driver, prefix)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize tables: %w", err)
	}

	var maxSeq uint64
	query := fmt.Sprintf("SELECT COALESCE(MAX(sequence), 0) FROM %smessages", prefix)
	err = backend.QueryRow(query).Scan(&maxSeq)
	if err != nil {
		return nil, fmt.Errorf("failed to get max sequence: %w", err)
	}

	return &SQLMessageStore{
		backend: backend,
		prefix:  prefix,
		nextSeq: maxSeq,
	}, nil
}

func initializeTables(dbConn *sql.DB, driver string, prefix string) error {
	// Standard schema that works across drivers
	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %smessages (
			id TEXT PRIMARY KEY,
			room_id TEXT,
			sender_id TEXT,
			target_id TEXT,
			type TEXT,
			payload BLOB,
			sequence INTEGER,
			timestamp DATETIME
		);
		CREATE TABLE IF NOT EXISTS %sdelivery (
			message_id TEXT,
			user_id TEXT,
			state TEXT,
			PRIMARY KEY (message_id, user_id)
		);
	`, prefix, prefix)

	_, err := dbConn.Exec(query)
	if err != nil {
		return err
	}

	// Add indexes if they don't exist
	_, _ = dbConn.Exec(fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%smessages_sender ON %smessages(sender_id)", prefix, prefix))
	_, _ = dbConn.Exec(fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%smessages_room ON %smessages(room_id)", prefix, prefix))
	_, _ = dbConn.Exec(fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%smessages_sequence ON %smessages(sequence)", prefix, prefix))

	return nil
}

func (s *SQLMessageStore) SaveMessage(ctx context.Context, msg core.Message, recipients []string) (core.Message, error) {
	if msg.ID == "" {
		msg.ID = fmt.Sprintf("msg-%d", time.Now().UnixNano())
	}
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}
	if msg.Sequence == 0 {
		s.nextSeq++
		msg.Sequence = s.nextSeq
	} else if msg.Sequence > s.nextSeq {
		s.nextSeq = msg.Sequence
	}

	// Try to get transaction from context
	tx := db.GetTx(ctx)
	if tx != nil {
		return s.saveWithTx(ctx, tx, msg, recipients)
	}

	// Start new transaction
	newTx, err := s.backend.Begin()
	if err != nil {
		return msg, err
	}
	defer newTx.Rollback()

	saved, err := s.saveWithTx(ctx, newTx, msg, recipients)
	if err != nil {
		return saved, err
	}

	return saved, newTx.Commit()
}

func (s *SQLMessageStore) saveWithTx(ctx context.Context, tx *sql.Tx, msg core.Message, recipients []string) (core.Message, error) {
	// Select the correct upsert query based on driver
	var query string
	switch s.backend.Driver {
	case db.SQLite:
		query = sqlite.UpsertMessage
	case db.Postgres:
		query = postgres.UpsertMessage
	case db.MySQL:
		query = mysql.UpsertMessage
	default:
		return msg, fmt.Errorf("unsupported driver for upsert: %s", s.backend.Driver)
	}

	// Inject prefix into query (this assumes the constants in driver packages are templateable or we replace them)
	// For simplicity in this demo, we assume the table name in constants is "messages" and we replace it
	query = strings.ReplaceAll(query, "INSERT INTO messages", "INSERT INTO "+s.prefix+"messages")
	query = strings.ReplaceAll(query, "ON CONFLICT(id) DO UPDATE", "ON CONFLICT(id) DO UPDATE") // No change needed for conflict

	_, err := tx.ExecContext(ctx, query, msg.ID, msg.RoomID, msg.SenderID, msg.TargetID, msg.Type, msg.Payload, msg.Sequence, msg.Timestamp)
	if err != nil {
		return msg, err
	}

	for _, recipient := range recipients {
		deliveryTable := s.prefix + "delivery"
		if s.backend.Driver == db.Postgres {
			_, err = tx.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (message_id, user_id, state) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING", deliveryTable), msg.ID, recipient, string(DeliveryPending))
		} else if s.backend.Driver == db.MySQL {
			_, err = tx.ExecContext(ctx, fmt.Sprintf("INSERT IGNORE INTO %s (message_id, user_id, state) VALUES (?, ?, ?)", deliveryTable), msg.ID, recipient, string(DeliveryPending))
		} else {
			_, err = tx.ExecContext(ctx, fmt.Sprintf("INSERT OR IGNORE INTO %s (message_id, user_id, state) VALUES (?, ?, ?)", deliveryTable), msg.ID, recipient, string(DeliveryPending))
		}
		if err != nil {
			return msg, err
		}
	}

	return msg, nil
}

func (s *SQLMessageStore) MarkDelivered(ctx context.Context, messageID, userID string) error {
	deliveryTable := s.prefix + "delivery"
	dbConn := s.conn(ctx)
	if s.backend.Driver == db.Postgres {
		_, err := dbConn.ExecContext(ctx, fmt.Sprintf("UPDATE %s SET state = $1 WHERE message_id = $2 AND user_id = $3 AND state != $4", deliveryTable), string(DeliveryDelivered), messageID, userID, string(DeliveryRead))
		return err
	}
	_, err := dbConn.ExecContext(ctx, fmt.Sprintf("UPDATE %s SET state = ? WHERE message_id = ? AND user_id = ? AND state != ?", deliveryTable), string(DeliveryDelivered), messageID, userID, string(DeliveryRead))
	return err
}

func (s *SQLMessageStore) PendingForUser(ctx context.Context, userID string) []core.Message {
	query := fmt.Sprintf(`SELECT m.id, m.room_id, m.sender_id, m.target_id, m.type, m.payload, m.sequence, m.timestamp
		FROM %smessages m
		JOIN %sdelivery d ON m.id = d.message_id
		WHERE d.user_id = ? AND d.state = ?`, s.prefix, s.prefix)
	
	if s.backend.Driver == db.Postgres {
		query = fmt.Sprintf(`SELECT m.id, m.room_id, m.sender_id, m.target_id, m.type, m.payload, m.sequence, m.timestamp
			FROM %smessages m
			JOIN %sdelivery d ON m.id = d.message_id
			WHERE d.user_id = $1 AND d.state = $2`, s.prefix, s.prefix)
	}

	rows, err := s.conn(ctx).QueryContext(ctx, query, userID, string(DeliveryPending))
	if err != nil {
		return nil
	}
	defer rows.Close()

	return s.scanMessages(rows)
}

func (s *SQLMessageStore) SyncForUserAfterSequence(ctx context.Context, userID string, after uint64, limit int) ([]core.Message, bool) {
	if limit <= 0 {
		limit = 100
	}

	query := fmt.Sprintf(`SELECT DISTINCT m.id, m.room_id, m.sender_id, m.target_id, m.type, m.payload, m.sequence, m.timestamp
		FROM %smessages m
		LEFT JOIN %sdelivery d ON m.id = d.message_id
		WHERE (m.sender_id = ? OR d.user_id = ?) AND m.sequence > ?
		ORDER BY m.sequence ASC
		LIMIT ?`, s.prefix, s.prefix)
	
	if s.backend.Driver == db.Postgres {
		query = fmt.Sprintf(`SELECT DISTINCT m.id, m.room_id, m.sender_id, m.target_id, m.type, m.payload, m.sequence, m.timestamp
			FROM %smessages m
			LEFT JOIN %sdelivery d ON m.id = d.message_id
			WHERE (m.sender_id = $1 OR d.user_id = $2) AND m.sequence > $3
			ORDER BY m.sequence ASC
			LIMIT $4`, s.prefix, s.prefix)
	}

	rows, err := s.conn(ctx).QueryContext(ctx, query, userID, userID, after, limit+1)
	if err != nil {
		return nil, false
	}
	defer rows.Close()

	messages := s.scanMessages(rows)
	hasMore := len(messages) > limit
	if hasMore {
		messages = messages[:limit]
	}
	return messages, hasMore
}

func (s *SQLMessageStore) LatestSequenceForUser(ctx context.Context, userID string) uint64 {
	var latest uint64
	query := fmt.Sprintf(`SELECT COALESCE(MAX(m.sequence), 0) FROM %smessages m
		LEFT JOIN %sdelivery d ON m.id = d.message_id
		WHERE m.sender_id = ? OR d.user_id = ?`, s.prefix, s.prefix)
	
	if s.backend.Driver == db.Postgres {
		query = fmt.Sprintf(`SELECT COALESCE(MAX(m.sequence), 0) FROM %smessages m
			LEFT JOIN %sdelivery d ON m.id = d.message_id
			WHERE m.sender_id = $1 OR d.user_id = $2`, s.prefix, s.prefix)
	}

	err := s.conn(ctx).QueryRowContext(ctx, query, userID, userID).Scan(&latest)
	if err != nil {
		return 0
	}
	return latest
}

func (s *SQLMessageStore) GetConversation(ctx context.Context, userID, contactID string, limit int) ([]core.Message, error) {
	if limit <= 0 {
		limit = 50
	}

	query := fmt.Sprintf(`SELECT id, room_id, sender_id, target_id, type, payload, sequence, timestamp
		FROM %smessages
		WHERE (sender_id = ? AND target_id = ?) OR (sender_id = ? AND target_id = ?)
		ORDER BY timestamp DESC
		LIMIT ?`, s.prefix)

	if s.backend.Driver == db.Postgres {
		query = fmt.Sprintf(`SELECT id, room_id, sender_id, target_id, type, payload, sequence, timestamp
			FROM %smessages
			WHERE (sender_id = $1 AND target_id = $2) OR (sender_id = $2 AND target_id = $1)
			ORDER BY timestamp DESC
			LIMIT $3`, s.prefix)
	}

	rows, err := s.conn(ctx).QueryContext(ctx, query, userID, contactID, contactID, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	messages := s.scanMessages(rows)
	// reverse to chronological order
	slices.Reverse(messages)
	return messages, nil
}

func (s *SQLMessageStore) SyncForUserSince(ctx context.Context, userID string, since time.Time) []core.Message {
	query := fmt.Sprintf(`SELECT DISTINCT m.id, m.room_id, m.sender_id, m.target_id, m.type, m.payload, m.sequence, m.timestamp
		FROM %smessages m
		LEFT JOIN %sdelivery d ON m.id = d.message_id
		WHERE (m.sender_id = ? OR d.user_id = ?) AND m.timestamp > ?`, s.prefix, s.prefix)
	
	if s.backend.Driver == db.Postgres {
		query = fmt.Sprintf(`SELECT DISTINCT m.id, m.room_id, m.sender_id, m.target_id, m.type, m.payload, m.sequence, m.timestamp
			FROM %smessages m
			LEFT JOIN %sdelivery d ON m.id = d.message_id
			WHERE (m.sender_id = $1 OR d.user_id = $2) AND m.timestamp > $3`, s.prefix, s.prefix)
	}

	rows, err := s.conn(ctx).QueryContext(ctx, query, userID, userID, since)
	if err != nil {
		return nil
	}
	defer rows.Close()

	return s.scanMessages(rows)
}

// conn returns either the transaction in the context or the base backend.
func (s *SQLMessageStore) conn(ctx context.Context) interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
} {
	if tx := db.GetTx(ctx); tx != nil {
		return tx
	}
	return s.backend
}

func (s *SQLMessageStore) scanMessages(rows *sql.Rows) []core.Message {
	var messages []core.Message
	for rows.Next() {
		var msg core.Message
		err := rows.Scan(&msg.ID, &msg.RoomID, &msg.SenderID, &msg.TargetID, &msg.Type, &msg.Payload, &msg.Sequence, &msg.Timestamp)
		if err != nil {
			continue
		}
		messages = append(messages, msg)
	}
	return messages
}
