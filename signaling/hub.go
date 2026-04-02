package signaling

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/Aswanidev-vs/mana/core"
	"github.com/Aswanidev-vs/mana/ws"
)

// SignalHandler processes incoming signaling messages.
type SignalHandler func(signal core.Signal)

// Peer represents a connected client in the framework.
type Peer struct {
	ID       string
	UserID   string
	DeviceID string
	Username string
	RoomID   string
	Conn     ws.Conn
	Context  context.Context
	Cancel   context.CancelFunc
}

// Hub manages all peer connections, signaling, and event broadcasting.
type Hub struct {
	mu        sync.RWMutex
	peers     map[string]*Peer
	userPeers map[string]map[string]bool
	handlers  map[core.SignalType][]func(core.Signal)
	roomPeers map[string]map[string]bool

	// Hub-level hooks
	onJoin  func(peerID, roomID string)
	onLeave func(peerID, roomID string)

	// Participant state tracking for video calls
	participantState map[string]map[string]*ParticipantState
}

// ParticipantState tracks per-participant signaling state.
type ParticipantState struct {
	IsMuted    bool
	CameraOn   bool
	ScreenOn   bool
	IsOnline   bool
	IsPinned   bool
	AudioLevel float64
}

// NewHub creates a new signaling Hub.
func NewHub() *Hub {
	return &Hub{
		peers:            make(map[string]*Peer),
		userPeers:        make(map[string]map[string]bool),
		handlers:         make(map[core.SignalType][]func(core.Signal)),
		roomPeers:        make(map[string]map[string]bool),
		participantState: make(map[string]map[string]*ParticipantState),
	}
}

// RegisterPeer adds a peer connection to the hub.
func (h *Hub) RegisterPeer(peer *Peer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.peers[peer.ID] = peer
	if peer.UserID != "" {
		if h.userPeers[peer.UserID] == nil {
			h.userPeers[peer.UserID] = make(map[string]bool)
		}
		h.userPeers[peer.UserID][peer.ID] = true
	}
}

// SetOnJoin registers a callback for when a peer joins a room.
func (h *Hub) SetOnJoin(handler func(peerID, roomID string)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onJoin = handler
}

// SetOnLeave registers a callback for when a peer leaves a room.
func (h *Hub) SetOnLeave(handler func(peerID, roomID string)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onLeave = handler
}

// UnregisterPeer removes a peer from the hub and all rooms.
func (h *Hub) UnregisterPeer(id string) {
	h.mu.Lock()
	peer, ok := h.peers[id]
	if !ok {
		h.mu.Unlock()
		return
	}
	delete(h.peers, id)
	if peer.UserID != "" {
		if peers := h.userPeers[peer.UserID]; peers != nil {
			delete(peers, id)
			if len(peers) == 0 {
				delete(h.userPeers, peer.UserID)
			}
		}
	}

	// Remove from all room peer mappings and trigger onLeave
	for roomID, peers := range h.roomPeers {
		if _, exists := peers[id]; exists {
			delete(peers, id)
			if h.onLeave != nil {
				h.onLeave(id, roomID)
			}
			if len(peers) == 0 {
				delete(h.roomPeers, roomID)
			}
		}
	}

	// Clean up participant state
	for roomID, participants := range h.participantState {
		delete(participants, id)
		if len(participants) == 0 {
			delete(h.participantState, roomID)
		}
	}

	h.mu.Unlock()

	if peer.Cancel != nil {
		peer.Cancel()
	}
}

// AddPeerToRoom tracks that a peer has joined a room (for scoped signaling).
func (h *Hub) AddPeerToRoom(roomID, peerID string) {
	h.mu.Lock()
	if h.roomPeers[roomID] == nil {
		h.roomPeers[roomID] = make(map[string]bool)
	}
	h.roomPeers[roomID][peerID] = true

	// Initialize participant state
	if h.participantState[roomID] == nil {
		h.participantState[roomID] = make(map[string]*ParticipantState)
	}
	h.participantState[roomID][peerID] = &ParticipantState{
		CameraOn: true,
		IsOnline: true,
	}

	// Trigger onJoin hook
	handler := h.onJoin
	h.mu.Unlock()

	if handler != nil {
		handler(peerID, roomID)
	}
}

// RemovePeerFromRoom removes a peer from a room's signaling scope.
func (h *Hub) RemovePeerFromRoom(roomID, peerID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if peers, ok := h.roomPeers[roomID]; ok {
		delete(peers, peerID)
		if len(peers) == 0 {
			delete(h.roomPeers, roomID)
		}
	}
	if participants, ok := h.participantState[roomID]; ok {
		delete(participants, peerID)
	}
}

// On registers a handler for a specific signal type.
func (h *Hub) On(sigType core.SignalType, handler func(core.Signal)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.handlers[sigType] = append(h.handlers[sigType], handler)
}

// Send transmits a signal to a specific peer.
func (h *Hub) Send(ctx context.Context, signal core.Signal) error {
	h.mu.RLock()
	peer, ok := h.peers[signal.To]
	var userPeerIDs map[string]bool
	if !ok {
		if peers := h.userPeers[signal.To]; len(peers) > 0 {
			userPeerIDs = make(map[string]bool, len(peers))
			for peerID := range peers {
				userPeerIDs[peerID] = true
			}
		}
	}
	h.mu.RUnlock()

	if !ok && len(userPeerIDs) == 0 {
		return fmt.Errorf("peer %s not found", signal.To)
	}

	data, err := json.Marshal(signal)
	if err != nil {
		return fmt.Errorf("marshal signal: %w", err)
	}

	if len(userPeerIDs) > 0 {
		for peerID := range userPeerIDs {
			h.mu.RLock()
			targetPeer := h.peers[peerID]
			h.mu.RUnlock()
			if targetPeer != nil {
				_ = targetPeer.Conn.Write(ctx, data)
			}
		}
		return nil
	}

	return peer.Conn.Write(ctx, data)
}

// BroadcastToRoom sends a signal to all peers in a specific room (except sender).
func (h *Hub) BroadcastToRoom(ctx context.Context, roomID, senderID string, signal core.Signal) error {
	h.mu.RLock()
	defer h.mu.RUnlock()

	peers, ok := h.roomPeers[roomID]
	if !ok {
		return nil // no peers in room
	}

	data, err := json.Marshal(signal)
	if err != nil {
		return fmt.Errorf("marshal signal: %w", err)
	}

	for peerID := range peers {
		if peerID == senderID {
			continue
		}
		if peer, exists := h.peers[peerID]; exists {
			_ = peer.Conn.Write(ctx, data)
		}
	}
	return nil
}

// Broadcast sends a signal to all peers (except sender). Use BroadcastToRoom for room-scoped signals.
func (h *Hub) Broadcast(ctx context.Context, roomID, senderID string, signal core.Signal) error {
	// If roomID is provided, use room-scoped broadcast
	if roomID != "" {
		return h.BroadcastToRoom(ctx, roomID, senderID, signal)
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	data, err := json.Marshal(signal)
	if err != nil {
		return fmt.Errorf("marshal signal: %w", err)
	}

	for id, peer := range h.peers {
		if id == senderID {
			continue
		}
		_ = peer.Conn.Write(ctx, data)
	}
	return nil
}

// HandleMessage processes an incoming raw WebSocket message as a signal.
// It forwards offer/answer/candidate signals to the target peer and invokes handlers.
func (h *Hub) HandleMessage(ctx context.Context, data []byte) error {
	var signal core.Signal
	if err := json.Unmarshal(data, &signal); err != nil {
		return fmt.Errorf("unmarshal signal: %w", err)
	}

	// If signal has a target peer, forward directly
	if signal.To != "" {
		if err := h.Send(ctx, signal); err != nil {
			_ = err
		}
	}

	h.Dispatch(signal)
	return nil
}

// Dispatch invokes registered handlers for a parsed signal without forwarding it.
func (h *Hub) Dispatch(signal core.Signal) {
	h.mu.RLock()
	handlers := h.handlers[signal.Type]
	h.mu.RUnlock()

	for _, handler := range handlers {
		handler(signal)
	}
}

// PeerCount returns the number of registered peers.
func (h *Hub) PeerCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.peers)
}

// UserPeerIDs returns the active peer/session IDs for a user.
func (h *Hub) UserPeerIDs(userID string) []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	peers := h.userPeers[userID]
	if len(peers) == 0 {
		return nil
	}

	ids := make([]string, 0, len(peers))
	for peerID := range peers {
		ids = append(ids, peerID)
	}
	return ids
}

// GetParticipantState returns the participant state for a peer in a room.
func (h *Hub) GetParticipantState(roomID, peerID string) (*ParticipantState, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if participants, ok := h.participantState[roomID]; ok {
		if state, ok := participants[peerID]; ok {
			return state, true
		}
	}
	return nil, false
}

// UpdateParticipantState updates the participant state for a peer in a room.
func (h *Hub) UpdateParticipantState(roomID, peerID string, updates map[string]interface{}) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.participantState[roomID] == nil {
		h.participantState[roomID] = make(map[string]*ParticipantState)
	}
	if h.participantState[roomID][peerID] == nil {
		h.participantState[roomID][peerID] = &ParticipantState{CameraOn: true, IsOnline: true}
	}

	state := h.participantState[roomID][peerID]
	if muted, ok := updates["is_muted"].(bool); ok {
		state.IsMuted = muted
	}
	if cameraOn, ok := updates["camera_on"].(bool); ok {
		state.CameraOn = cameraOn
	}
	if screenOn, ok := updates["screen_on"].(bool); ok {
		state.ScreenOn = screenOn
	}
	if online, ok := updates["is_online"].(bool); ok {
		state.IsOnline = online
	}
	if pinned, ok := updates["is_pinned"].(bool); ok {
		state.IsPinned = pinned
	}
	if level, ok := updates["audio_level"].(float64); ok {
		state.AudioLevel = level
	}
}
