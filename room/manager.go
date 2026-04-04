package room

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Aswanidev-vs/mana/core"
	"github.com/Aswanidev-vs/mana/ws"
)

// Room represents a communication room with members.
type Room struct {
	mu               sync.RWMutex
	info             core.RoomInfo
	members          map[string]ws.Conn
	sessionUsers     map[string]core.User
	userSessionCount map[string]int
}

// NewRoom creates a new room with the given metadata.
func NewRoom(id, name, roomType, ownerID string) *Room {
	return &Room{
		info: core.RoomInfo{
			ID:        id,
			Name:      name,
			Type:      roomType,
			OwnerID:   ownerID,
			CreatedAt: time.Now(),
			Members:   make([]core.User, 0),
		},
		members:          make(map[string]ws.Conn),
		sessionUsers:     make(map[string]core.User),
		userSessionCount: make(map[string]int),
	}
}

// ID returns the room identifier.
func (r *Room) ID() string {
	return r.info.ID
}

// Name returns the room name.
func (r *Room) Name() string {
	return r.info.Name
}

// AddMember adds a user to the room.
func (r *Room) AddMember(user core.User, conn ws.Conn) error {
	return r.AddMemberSession(user.ID, user, conn)
}

// AddMemberSession adds a device/session to the room while keeping room member metadata unique by user.
func (r *Room) AddMemberSession(sessionID string, user core.User, conn ws.Conn) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.members[sessionID]; exists {
		return fmt.Errorf("session %s already in room %s", sessionID, r.info.ID)
	}

	r.members[sessionID] = conn
	r.sessionUsers[sessionID] = user
	user.Online = true
	if r.userSessionCount[user.ID] == 0 {
		r.info.Members = append(r.info.Members, user)
	}
	r.userSessionCount[user.ID]++
	return nil
}

// RemoveMember removes a user from the room.
func (r *Room) RemoveMember(sessionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	user, exists := r.sessionUsers[sessionID]
	if !exists {
		return fmt.Errorf("session %s not in room %s", sessionID, r.info.ID)
	}

	delete(r.members, sessionID)
	delete(r.sessionUsers, sessionID)

	if r.userSessionCount[user.ID] > 0 {
		r.userSessionCount[user.ID]--
	}
	if r.userSessionCount[user.ID] == 0 {
		delete(r.userSessionCount, user.ID)
		for i, m := range r.info.Members {
			if m.ID == user.ID {
				r.info.Members = append(r.info.Members[:i], r.info.Members[i+1:]...)
				break
			}
		}
	}
	return nil
}

// Broadcast sends a message to all members except the sender.
func (r *Room) Broadcast(ctx context.Context, senderSessionID string, data []byte) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for sessionID, conn := range r.members {
		if sessionID == senderSessionID {
			continue
		}
		if err := conn.Write(ctx, data); err != nil {
			return fmt.Errorf("broadcast to %s: %w", sessionID, err)
		}
	}
	return nil
}

// Send sends a message to a specific member.
func (r *Room) Send(ctx context.Context, userID string, data []byte) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	sent := false
	for sessionID, user := range r.sessionUsers {
		if user.ID != userID {
			continue
		}
		conn := r.members[sessionID]
		if conn == nil {
			continue
		}
		if err := conn.Write(ctx, data); err != nil {
			return err
		}
		sent = true
	}
	if !sent {
		return fmt.Errorf("user %s not in room", userID)
	}
	return nil
}

// Members returns a copy of the room member list.
func (r *Room) Members() []core.User {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]core.User, len(r.info.Members))
	copy(result, r.info.Members)
	return result
}

// MemberCount returns the number of members in the room.
func (r *Room) MemberCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.members)
}

// Manager manages multiple rooms.
type Manager struct {
	mu    sync.RWMutex
	rooms map[string]*Room
}

// NewManager creates a new room Manager.
func NewManager() *Manager {
	return &Manager{
		rooms: make(map[string]*Room),
	}
}

// Create creates a new room with metadata.
func (m *Manager) Create(id, name, roomType, ownerID string) *Room {
	m.mu.Lock()
	defer m.mu.Unlock()
 
	room := NewRoom(id, name, roomType, ownerID)
	m.rooms[id] = room
	return room
}

// Get retrieves a room by ID.
func (m *Manager) Get(id string) (*Room, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	room, ok := m.rooms[id]
	if !ok {
		return nil, fmt.Errorf("room %s not found", id)
	}
	return room, nil
}

// Delete removes a room.
func (m *Manager) Delete(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rooms, id)
}

// Join adds a user to a room, creating the room if it doesn't exist.
func (m *Manager) Join(roomID string, user core.User, conn ws.Conn) error {
	return m.JoinSession(roomID, user.ID, user, conn)
}

// JoinSession adds a specific session to a room, creating the room if it doesn't exist.
func (m *Manager) JoinSession(roomID, sessionID string, user core.User, conn ws.Conn) error {
	m.mu.Lock()
	room, ok := m.rooms[roomID]
	if !ok {
		room = NewRoom(roomID, roomID, "group", user.ID)
		m.rooms[roomID] = room
	}
	m.mu.Unlock()

	return room.AddMemberSession(sessionID, user, conn)
}

// Leave removes a user from a room.
func (m *Manager) Leave(roomID, userID string) error {
	room, err := m.Get(roomID)
	if err != nil {
		return err
	}
	return room.RemoveMember(userID)
}

// List returns all room infos.
func (m *Manager) List() []core.RoomInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	infos := make([]core.RoomInfo, 0, len(m.rooms))
	for _, r := range m.rooms {
		infos = append(infos, core.RoomInfo{
			ID:        r.info.ID,
			Name:      r.info.Name,
			Type:      r.info.Type,
			OwnerID:   r.info.OwnerID,
			CreatedAt: r.info.CreatedAt,
			Members:   r.Members(),
		})
	}
	return infos
}

// UserSession tracks a user's connection and joined rooms.
type UserSession struct {
	SessionID string
	UserID    string
	Username  string
	DeviceID  string
	Conn      ws.Conn
	Rooms     map[string]bool
	Connected time.Time
	mu        sync.RWMutex
}

// NewUserSession creates a new user session.
func NewUserSession(userID, username string, conn ws.Conn) *UserSession {
	return NewDeviceSession(userID, userID, username, "", conn)
}

// NewDeviceSession creates a new user session bound to a specific device/session identifier.
func NewDeviceSession(sessionID, userID, username, deviceID string, conn ws.Conn) *UserSession {
	return &UserSession{
		SessionID: sessionID,
		UserID:    userID,
		Username:  username,
		DeviceID:  deviceID,
		Conn:      conn,
		Rooms:     make(map[string]bool),
		Connected: time.Now(),
	}
}

// AddRoom tracks that the user joined a room.
func (s *UserSession) AddRoom(roomID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Rooms[roomID] = true
}

// RemoveRoom tracks that the user left a room.
func (s *UserSession) RemoveRoom(roomID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Rooms, roomID)
}

// RoomIDs returns the list of rooms the user has joined.
func (s *UserSession) RoomIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := make([]string, 0, len(s.Rooms))
	for id := range s.Rooms {
		ids = append(ids, id)
	}
	return ids
}
