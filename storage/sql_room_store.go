package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Aswanidev-vs/mana/core"
	"github.com/Aswanidev-vs/mana/storage/db"
)

type SQLRoomStore struct {
	backend *db.Backend
	prefix  string
}

func NewSQLRoomStore(backend *db.Backend) (*SQLRoomStore, error) {
	return NewSQLRoomStoreWithPrefix(backend, "")
}

func NewSQLRoomStoreWithPrefix(backend *db.Backend, prefix string) (*SQLRoomStore, error) {
	if prefix != "" && !strings.HasSuffix(prefix, "_") {
		prefix += "_"
	}

	// Initialize tables
	err := initializeRoomTables(backend.DB, backend.Driver, prefix)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize room tables: %w", err)
	}

	return &SQLRoomStore{
		backend: backend,
		prefix:  prefix,
	}, nil
}

func initializeRoomTables(dbConn *sql.DB, driver string, prefix string) error {
	queries := []string{
		fmt.Sprintf(`
			CREATE TABLE IF NOT EXISTS %srooms (
				room_id TEXT PRIMARY KEY,
				name TEXT,
				type TEXT,
				owner_id TEXT,
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP
			)
		`, prefix),
		fmt.Sprintf(`
			CREATE TABLE IF NOT EXISTS %sroom_members (
				room_id TEXT,
				user_id TEXT,
				joined_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				PRIMARY KEY (room_id, user_id)
			)
		`, prefix),
	}

	for _, query := range queries {
		_, err := dbConn.Exec(query)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLRoomStore) CreateRoom(ctx context.Context, name, roomType, ownerID string, memberIDs []string) (string, error) {
	roomID := fmt.Sprintf("rm-%d", time.Now().UnixNano())
	
	query := fmt.Sprintf("INSERT INTO %srooms (room_id, name, type, owner_id) VALUES (?, ?, ?, ?)", s.prefix)
	if s.backend.Driver == db.Postgres {
		query = fmt.Sprintf("INSERT INTO %srooms (room_id, name, type, owner_id) VALUES ($1, $2, $3, $4)", s.prefix)
	}

	_, err := s.conn(ctx).ExecContext(ctx, query, roomID, name, roomType, ownerID)
	if err != nil {
		return "", err
	}

	// Add owner as member
	memberIDs = append(memberIDs, ownerID)
	for _, userID := range uniqueStrings(memberIDs) {
		_ = s.AddMember(ctx, roomID, userID)
	}

	return roomID, nil
}

func (s *SQLRoomStore) GetRoom(ctx context.Context, roomID string) (core.RoomInfo, error) {
	query := fmt.Sprintf("SELECT room_id, name, type, owner_id, created_at FROM %srooms WHERE room_id = ?", s.prefix)
	if s.backend.Driver == db.Postgres {
		query = fmt.Sprintf("SELECT room_id, name, type, owner_id, created_at FROM %srooms WHERE room_id = $1", s.prefix)
	}

	var room core.RoomInfo
	err := s.conn(ctx).QueryRowContext(ctx, query, roomID).Scan(&room.ID, &room.Name, &room.Type, &room.OwnerID, &room.CreatedAt)
	if err != nil {
		return core.RoomInfo{}, err
	}

	// Fetch members
	membersQuery := fmt.Sprintf("SELECT user_id FROM %sroom_members WHERE room_id = ?", s.prefix)
	if s.backend.Driver == db.Postgres {
		membersQuery = fmt.Sprintf("SELECT user_id FROM %sroom_members WHERE room_id = $1", s.prefix)
	}

	rows, err := s.conn(ctx).QueryContext(ctx, membersQuery, roomID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var userID string
			if err := rows.Scan(&userID); err == nil {
				room.Members = append(room.Members, core.User{ID: userID})
			}
		}
	}

	return room, nil
}

func (s *SQLRoomStore) ListUserRooms(ctx context.Context, userID string) ([]core.RoomInfo, error) {
	query := fmt.Sprintf(`
		SELECT r.room_id, r.name, r.type, r.owner_id, r.created_at 
		FROM %srooms r
		JOIN %sroom_members m ON r.room_id = m.room_id
		WHERE m.user_id = ?
	`, s.prefix, s.prefix)
	
	if s.backend.Driver == db.Postgres {
		query = strings.ReplaceAll(query, "?", "$1")
	}

	rows, err := s.conn(ctx).QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rooms []core.RoomInfo
	for rows.Next() {
		var room core.RoomInfo
		if err := rows.Scan(&room.ID, &room.Name, &room.Type, &room.OwnerID, &room.CreatedAt); err == nil {
			rooms = append(rooms, room)
		}
	}
	return rooms, nil
}

func (s *SQLRoomStore) AddMember(ctx context.Context, roomID, userID string) error {
	query := fmt.Sprintf("INSERT OR IGNORE INTO %sroom_members (room_id, user_id) VALUES (?, ?)", s.prefix)
	if s.backend.Driver == db.Postgres {
		query = fmt.Sprintf("INSERT INTO %sroom_members (room_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING", s.prefix)
	}

	_, err := s.conn(ctx).ExecContext(ctx, query, roomID, userID)
	return err
}

func (s *SQLRoomStore) RemoveMember(ctx context.Context, roomID, userID string) error {
	query := fmt.Sprintf("DELETE FROM %sroom_members WHERE room_id = ? AND user_id = ?", s.prefix)
	if s.backend.Driver == db.Postgres {
		query = fmt.Sprintf("DELETE FROM %sroom_members WHERE room_id = $1 AND user_id = $2", s.prefix)
	}

	_, err := s.conn(ctx).ExecContext(ctx, query, roomID, userID)
	return err
}

func (s *SQLRoomStore) conn(ctx context.Context) interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
} {
	if tx := db.GetTx(ctx); tx != nil {
		return tx
	}
	return s.backend
}
