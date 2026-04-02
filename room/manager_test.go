package room

import (
	"context"
	"testing"

	"github.com/Aswanidev-vs/mana/core"
)

// mockConn implements ws.Conn for testing.
type mockConn struct {
	messages [][]byte
	closed   bool
}

func newMockConn() *mockConn {
	return &mockConn{messages: make([][]byte, 0)}
}

func (m *mockConn) Read(ctx context.Context) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (m *mockConn) Write(ctx context.Context, data []byte) error {
	m.messages = append(m.messages, data)
	return nil
}

func (m *mockConn) Close() error {
	m.closed = true
	return nil
}

func (m *mockConn) Messages() []string {
	result := make([]string, len(m.messages))
	for i, msg := range m.messages {
		result[i] = string(msg)
	}
	return result
}

func TestRoomAddRemoveMember(t *testing.T) {
	room := NewRoom("room1", "Test Room")
	user := core.User{ID: "user1", Username: "alice"}
	conn := newMockConn()

	if err := room.AddMember(user, conn); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	if room.MemberCount() != 1 {
		t.Fatalf("expected 1 member, got %d", room.MemberCount())
	}

	// Adding same user again should fail
	if err := room.AddMember(user, conn); err == nil {
		t.Fatal("expected error adding duplicate member")
	}

	if err := room.RemoveMember("user1"); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}

	if room.MemberCount() != 0 {
		t.Fatalf("expected 0 members, got %d", room.MemberCount())
	}

	// Removing non-existent user should fail
	if err := room.RemoveMember("user1"); err == nil {
		t.Fatal("expected error removing non-existent member")
	}
}

func TestRoomBroadcast(t *testing.T) {
	room := NewRoom("room1", "Test Room")
	conn1 := newMockConn()
	conn2 := newMockConn()

	user1 := core.User{ID: "user1", Username: "alice"}
	user2 := core.User{ID: "user2", Username: "bob"}

	room.AddMember(user1, conn1)
	room.AddMember(user2, conn2)

	ctx := context.Background()
	msg := []byte("hello room")

	if err := room.Broadcast(ctx, "user1", msg); err != nil {
		t.Fatalf("Broadcast: %v", err)
	}

	// conn1 (sender) should not receive the message
	if len(conn1.Messages()) != 0 {
		t.Fatalf("sender should not receive broadcast, got %d messages", len(conn1.Messages()))
	}

	// conn2 should receive the message
	if len(conn2.Messages()) != 1 {
		t.Fatalf("expected 1 message on conn2, got %d", len(conn2.Messages()))
	}

	if conn2.Messages()[0] != "hello room" {
		t.Fatalf("expected 'hello room', got %q", conn2.Messages()[0])
	}
}

func TestRoomSend(t *testing.T) {
	room := NewRoom("room1", "Test Room")
	conn := newMockConn()
	user := core.User{ID: "user1", Username: "alice"}
	room.AddMember(user, conn)

	ctx := context.Background()
	if err := room.Send(ctx, "user1", []byte("direct msg")); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if len(conn.Messages()) != 1 {
		t.Fatalf("expected 1 message, got %d", len(conn.Messages()))
	}

	// Send to non-existent user should fail
	if err := room.Send(ctx, "ghost", []byte("msg")); err == nil {
		t.Fatal("expected error sending to non-existent user")
	}
}

func TestManagerCreateAndGet(t *testing.T) {
	mgr := NewManager()

	room := mgr.Create("r1", "Room 1")
	if room == nil {
		t.Fatal("Create returned nil")
	}

	got, err := mgr.Get("r1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.ID() != "r1" {
		t.Fatalf("expected r1, got %s", got.ID())
	}

	_, err = mgr.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent room")
	}
}

func TestManagerJoinAndLeave(t *testing.T) {
	mgr := NewManager()
	conn := newMockConn()
	user := core.User{ID: "user1", Username: "alice"}

	// Join creates room automatically
	if err := mgr.Join("r1", user, conn); err != nil {
		t.Fatalf("Join: %v", err)
	}

	room, _ := mgr.Get("r1")
	if room.MemberCount() != 1 {
		t.Fatalf("expected 1 member, got %d", room.MemberCount())
	}

	if err := mgr.Leave("r1", "user1"); err != nil {
		t.Fatalf("Leave: %v", err)
	}

	if room.MemberCount() != 0 {
		t.Fatalf("expected 0 members, got %d", room.MemberCount())
	}
}

func TestManagerList(t *testing.T) {
	mgr := NewManager()
	mgr.Create("r1", "Room 1")
	mgr.Create("r2", "Room 2")

	list := mgr.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 rooms, got %d", len(list))
	}
}

func TestManagerDelete(t *testing.T) {
	mgr := NewManager()
	mgr.Create("r1", "Room 1")
	mgr.Delete("r1")

	_, err := mgr.Get("r1")
	if err == nil {
		t.Fatal("expected error after deleting room")
	}
}

func TestUserSession(t *testing.T) {
	conn := newMockConn()
	session := NewUserSession("u1", "alice", conn)

	session.AddRoom("r1")
	session.AddRoom("r2")

	rooms := session.RoomIDs()
	if len(rooms) != 2 {
		t.Fatalf("expected 2 rooms, got %d", len(rooms))
	}

	session.RemoveRoom("r1")
	rooms = session.RoomIDs()
	if len(rooms) != 1 {
		t.Fatalf("expected 1 room after remove, got %d", len(rooms))
	}
}
