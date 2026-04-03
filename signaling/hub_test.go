package signaling

import (
	"context"
	"testing"
	"time"

	"github.com/Aswanidev-vs/mana/cluster"
	"github.com/Aswanidev-vs/mana/core"
	"github.com/Aswanidev-vs/mana/ws"
)

// mockConn implements ws.Conn for testing.
type mockConn struct {
	messages [][]byte
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

func (m *mockConn) Close() error { return nil }

// Ensure mockConn implements ws.Conn
var _ ws.Conn = (*mockConn)(nil)

func newTestPeer(id string, conn ws.Conn) *Peer {
	ctx, cancel := context.WithCancel(context.Background())
	return &Peer{
		ID:      id,
		Conn:    conn,
		Context: ctx,
		Cancel:  cancel,
	}
}

func TestHubRegisterUnregister(t *testing.T) {
	hub := NewHub()
	conn := newMockConn()
	peer := newTestPeer("peer1", conn)

	hub.RegisterPeer(peer)
	if hub.PeerCount() != 1 {
		t.Fatalf("expected 1 peer, got %d", hub.PeerCount())
	}

	hub.UnregisterPeer("peer1")
	if hub.PeerCount() != 0 {
		t.Fatalf("expected 0 peers, got %d", hub.PeerCount())
	}
}

func TestHubSend(t *testing.T) {
	hub := NewHub()
	conn := newMockConn()
	peer := newTestPeer("peer1", conn)
	hub.RegisterPeer(peer)

	ctx := context.Background()
	signal := core.Signal{
		Type:    core.SignalOffer,
		From:    "peer2",
		To:      "peer1",
		Payload: []byte("offer-sdp"),
	}

	if err := hub.Send(ctx, signal); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if len(conn.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(conn.messages))
	}
}

func TestHubSendToUnknownPeer(t *testing.T) {
	hub := NewHub()
	ctx := context.Background()
	signal := core.Signal{
		Type: core.SignalOffer,
		From: "peer1",
		To:   "unknown",
	}

	err := hub.Send(ctx, signal)
	if err == nil {
		t.Fatal("expected error sending to unknown peer")
	}
}

func TestHubOnHandler(t *testing.T) {
	hub := NewHub()
	received := false

	hub.On(core.SignalOffer, func(signal core.Signal) {
		received = true
	})

	ctx := context.Background()
	data := []byte(`{"type":"offer","from":"peer1","to":"peer2"}`)
	hub.HandleMessage(ctx, data)

	if !received {
		t.Fatal("handler was not called")
	}
}

func TestHubRoomBroadcast(t *testing.T) {
	hub := NewHub()
	conn1 := newMockConn()
	conn2 := newMockConn()
	conn3 := newMockConn()

	hub.RegisterPeer(newTestPeer("peer1", conn1))
	hub.RegisterPeer(newTestPeer("peer2", conn2))
	hub.RegisterPeer(newTestPeer("peer3", conn3))

	hub.AddPeerToRoom("room1", "peer1")
	hub.AddPeerToRoom("room1", "peer2")
	hub.AddPeerToRoom("room2", "peer3")

	ctx := context.Background()
	signal := core.Signal{
		Type:   core.SignalOffer,
		From:   "peer1",
		RoomID: "room1",
	}

	if err := hub.BroadcastToRoom(ctx, "room1", "peer1", signal); err != nil {
		t.Fatalf("BroadcastToRoom: %v", err)
	}

	// peer2 should receive the message (peer1 is sender, peer3 is in different room)
	if len(conn2.messages) != 1 {
		t.Fatalf("expected peer2 to receive 1 message, got %d", len(conn2.messages))
	}
	if len(conn3.messages) != 0 {
		t.Fatalf("expected peer3 to receive 0 messages, got %d", len(conn3.messages))
	}
}

func TestHubParticipantState(t *testing.T) {
	hub := NewHub()
	hub.AddPeerToRoom("room1", "peer1")

	// Update mute state
	hub.UpdateParticipantState("room1", "peer1", map[string]interface{}{
		"is_muted": true,
	})

	state, ok := hub.GetParticipantState("room1", "peer1")
	if !ok {
		t.Fatal("expected participant state to exist")
	}
	if !state.IsMuted {
		t.Fatal("expected participant to be muted")
	}

	// Update camera state
	hub.UpdateParticipantState("room1", "peer1", map[string]interface{}{
		"camera_on": false,
	})

	state, _ = hub.GetParticipantState("room1", "peer1")
	if state.CameraOn {
		t.Fatal("expected camera to be off")
	}
}

func TestHubClusterFanout(t *testing.T) {
	bus := cluster.NewMemoryBackend()
	hubA := NewHub()
	hubB := NewHub()

	if err := hubA.SetCluster("node-a", bus); err != nil {
		t.Fatalf("set cluster on hubA: %v", err)
	}
	if err := hubB.SetCluster("node-b", bus); err != nil {
		t.Fatalf("set cluster on hubB: %v", err)
	}

	connA := newMockConn()
	connB := newMockConn()
	hubA.RegisterPeer(&Peer{ID: "alice::web", UserID: "alice", Conn: connA})
	hubB.RegisterPeer(&Peer{ID: "bob::web", UserID: "bob", Conn: connB})

	if err := hubA.Send(context.Background(), core.Signal{
		Type: core.SignalMessage,
		From: "alice",
		To:   "bob",
	}); err != nil {
		t.Fatalf("cluster send: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for len(connB.messages) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(connB.messages) != 1 {
		t.Fatalf("expected remote node peer to receive 1 message, got %d", len(connB.messages))
	}
}

func TestHubHandleSignalForwardsDirectWithoutRawDecode(t *testing.T) {
	hub := NewHub()
	conn := newMockConn()
	hub.RegisterPeer(newTestPeer("peer1", conn))

	hub.HandleSignal(context.Background(), core.Signal{
		Type: core.SignalOffer,
		From: "peer2",
		To:   "peer1",
		SDP:  "v=0",
	})

	if len(conn.messages) != 1 {
		t.Fatalf("expected 1 direct signal, got %d", len(conn.messages))
	}
}
