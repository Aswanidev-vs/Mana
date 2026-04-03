package signaling

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Aswanidev-vs/mana/core"
	"github.com/Aswanidev-vs/mana/room"
	"github.com/Aswanidev-vs/mana/rtc"
	"github.com/Aswanidev-vs/mana/ws"
)

// MessageHandler processes incoming messages.
type MessageHandler func(msg core.Message)

// JoinHandler processes user join events.
type JoinHandler func(roomID string, user core.User)

// LeaveHandler processes user leave events.
type LeaveHandler func(roomID string, user core.User)

// RouterConfig holds configuration for the message router.
type RouterConfig struct {
	Hub         *Hub
	RoomManager *room.Manager
	CallManager *rtc.CallManager
	Logger      Logger
	RBAC        Authorizer
	OnMessage   MessageHandler
}

// Logger is a minimal logging interface.
type Logger interface {
	Info(format string, args ...interface{})
	Error(format string, args ...interface{})
	Warn(format string, args ...interface{})
	Debug(format string, args ...interface{})
}

// Authorizer checks permissions.
type Authorizer interface {
	Authorize(role string, perm string) bool
}

// Router handles high-level message routing (join, leave, message, mute, camera, pin).
type Router struct {
	config RouterConfig
}

// NewRouter creates a new message router.
func NewRouter(cfg RouterConfig) *Router {
	return &Router{config: cfg}
}

// HandleIncoming parses raw data and routes it to the appropriate handler.
func (r *Router) HandleIncoming(ctx context.Context, peer *Peer, userRole string, data []byte) {
	var signal core.Signal
	if err := json.Unmarshal(data, &signal); err == nil && signal.Type != "" {
		r.HandleSignal(ctx, peer, userRole, signal)
		return
	}

	// Fallback: raw Message type
	var msg core.Message
	if err := json.Unmarshal(data, &msg); err == nil {
		msg.SenderID = peer.UserID
		msg.Timestamp = time.Now()
		if r.config.OnMessage != nil {
			r.config.OnMessage(msg)
		}
	}
}

// HandleSignal routes an already-decoded signal to the appropriate handler.
func (r *Router) HandleSignal(ctx context.Context, peer *Peer, userRole string, signal core.Signal) {
	if signal.From == "" {
		signal.From = peer.UserID
		switch signal.Type {
		case core.SignalOffer, core.SignalAnswer, core.SignalCandidate, core.SignalKeyExchange, core.SignalICERestart:
			signal.From = peer.ID
		}
	}

	handled := true
	switch signal.Type {
	case core.SignalJoin:
		r.handleJoin(ctx, peer, userRole, signal)
	case core.SignalLeave:
		r.handleLeave(ctx, peer, signal)
	case core.SignalMessage:
		r.handleMessage(ctx, peer, userRole, signal)
	case core.SignalTyping:
		r.handleTyping(ctx, peer, signal)
	case core.SignalMute:
		r.handleParticipantState(ctx, peer, signal, "is_muted", false)
	case core.SignalCameraToggle:
		r.handleParticipantState(ctx, peer, signal, "camera_on", true)
	case core.SignalScreenStart:
		r.handleParticipantState(ctx, peer, signal, "screen_on", true)
	case core.SignalScreenStop:
		r.handleParticipantState(ctx, peer, signal, "screen_on", false)
	case core.SignalPin:
		r.handlePin(ctx, peer, signal)
	case core.SignalCallStart:
		r.handleCallStart(ctx, peer, userRole, signal)
	case core.SignalCallEnd:
		r.handleCallEnd(ctx, peer, userRole, signal)
	default:
		handled = false
	}

	if handled {
		r.config.Hub.Dispatch(signal)
		return
	}

	r.config.Hub.HandleSignal(ctx, signal)
}

func (r *Router) handleJoin(ctx context.Context, peer *Peer, userRole string, signal core.Signal) {
	if r.config.RBAC != nil && !r.config.RBAC.Authorize(userRole, "room:join") {
		sendError(ctx, peer.Conn, "permission denied: room:join")
		return
	}
	r.config.RoomManager.JoinSession(signal.RoomID, peer.ID, core.User{ID: peer.UserID, Username: peer.Username, Online: true}, peer.Conn)
	r.config.Hub.AddPeerToRoom(signal.RoomID, peer.ID)
	r.broadcastPresence(ctx, signal.RoomID, peer.ID, peer.UserID, peer.Username, true)
}

func (r *Router) handleLeave(ctx context.Context, peer *Peer, signal core.Signal) {
	r.config.RoomManager.Leave(signal.RoomID, peer.ID)
	r.config.Hub.RemovePeerFromRoom(signal.RoomID, peer.ID)
	r.broadcastPresence(ctx, signal.RoomID, peer.ID, peer.UserID, peer.Username, false)
}

func (r *Router) handleMessage(ctx context.Context, peer *Peer, userRole string, signal core.Signal) {
	if r.config.RBAC != nil && !r.config.RBAC.Authorize(userRole, "message:send") {
		sendError(ctx, peer.Conn, "permission denied: message:send")
		return
	}

	if r.config.OnMessage != nil {
		r.config.OnMessage(core.Message{
			Type:     "message",
			SenderID: peer.UserID,
			Payload:  signal.Payload,
			RoomID:   signal.RoomID,
			TargetID: signal.To,
			AckID:    signal.AckID,
		})
	}

	if signal.To != "" {
		_ = r.config.Hub.Send(ctx, signal)
	} else if signal.RoomID != "" {
		_ = r.config.Hub.BroadcastToRoom(ctx, signal.RoomID, peer.ID, signal)
	}
}

func (r *Router) handleParticipantState(ctx context.Context, peer *Peer, signal core.Signal, field string, defaultValue bool) {
	if signal.RoomID == "" {
		return
	}
	stateVal := defaultValue
	if len(signal.Payload) > 0 {
		json.Unmarshal(signal.Payload, &stateVal)
	}
	r.config.Hub.UpdateParticipantState(signal.RoomID, peer.ID, map[string]interface{}{field: stateVal})
	_ = r.config.Hub.BroadcastToRoom(ctx, signal.RoomID, peer.ID, signal)
}

func (r *Router) handlePin(ctx context.Context, peer *Peer, signal core.Signal) {
	if signal.RoomID != "" {
		_ = r.config.Hub.BroadcastToRoom(ctx, signal.RoomID, peer.ID, signal)
	}
}

func (r *Router) handleTyping(ctx context.Context, peer *Peer, signal core.Signal) {
	if signal.RoomID != "" {
		_ = r.config.Hub.BroadcastToRoom(ctx, signal.RoomID, peer.ID, signal)
	}
}

func (r *Router) handleCallStart(ctx context.Context, peer *Peer, userRole string, signal core.Signal) {
	if r.config.RBAC != nil && !r.config.RBAC.Authorize(userRole, "call:start") {
		sendError(ctx, peer.Conn, "permission denied: call:start")
		return
	}
	if signal.RoomID == "" {
		sendError(ctx, peer.Conn, "room_id required: call:start")
		return
	}

	callType := core.CallAudio
	if string(signal.Payload) == string(core.CallVideo) {
		callType = core.CallVideo
	}
	if r.config.CallManager != nil {
		r.config.CallManager.StartCall(callType, signal.RoomID, peer.UserID, signal.To)
	}

	if signal.To != "" {
		_ = r.config.Hub.Send(ctx, signal)
	} else {
		_ = r.config.Hub.BroadcastToRoom(ctx, signal.RoomID, peer.ID, signal)
	}
}

func (r *Router) handleCallEnd(ctx context.Context, peer *Peer, userRole string, signal core.Signal) {
	if r.config.RBAC != nil && !r.config.RBAC.Authorize(userRole, "call:end") {
		sendError(ctx, peer.Conn, "permission denied: call:end")
		return
	}
	if signal.RoomID == "" {
		sendError(ctx, peer.Conn, "room_id required: call:end")
		return
	}

	if r.config.CallManager != nil {
		_ = r.config.CallManager.EndCall(signal.RoomID)
	}

	if signal.To != "" {
		_ = r.config.Hub.Send(ctx, signal)
	} else {
		_ = r.config.Hub.BroadcastToRoom(ctx, signal.RoomID, peer.ID, signal)
	}
}

func (r *Router) broadcastPresence(ctx context.Context, roomID, senderSessionID, userID, username string, online bool) {
	presence := core.PresenceEvent{
		Type:     "presence",
		UserID:   userID,
		Username: username,
		RoomID:   roomID,
		Online:   online,
	}
	data, err := json.Marshal(presence)
	if err != nil {
		return
	}
	if rm, err := r.config.RoomManager.Get(roomID); err == nil {
		_ = rm.Broadcast(ctx, senderSessionID, data)
	}
}

func sendError(ctx context.Context, conn ws.Conn, errMsg string) {
	data, _ := json.Marshal(map[string]string{"type": "error", "error": errMsg})
	_ = conn.Write(ctx, data)
}
