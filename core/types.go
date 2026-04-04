package core

import "time"

// Message represents a communication message between peers.
type Message struct {
	ID        string    `json:"id"`
	Sequence  uint64    `json:"sequence,omitempty"`
	Type      string    `json:"type"`
	RoomID    string    `json:"room_id,omitempty"`
	SenderID  string    `json:"sender_id"`
	TargetID  string    `json:"target_id,omitempty"` // For 1:1 messaging
	Payload   []byte    `json:"payload"`
	Timestamp time.Time `json:"timestamp"`
	AckID     string    `json:"ack_id,omitempty"` // For message acknowledgments
}

// DeviceSyncBatch delivers historical messages to a reconnecting device.
type DeviceSyncBatch struct {
	Type      string    `json:"type"` // "message_sync"
	SessionID string    `json:"session_id,omitempty"`
	DeviceID  string    `json:"device_id,omitempty"`
	Cursor    uint64    `json:"cursor,omitempty"`
	HasMore   bool      `json:"has_more,omitempty"`
	Reason    string    `json:"reason,omitempty"`
	Messages  []Message `json:"messages"`
	Timestamp time.Time `json:"timestamp"`
}

// SyncRequest allows a reconnecting client to explicitly reconcile from a known cursor.
type SyncRequest struct {
	Type     string `json:"type"` // "sync_request"
	DeviceID string `json:"device_id,omitempty"`
	Cursor   uint64 `json:"cursor,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

// User represents a connected user.
type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Online   bool   `json:"online"`
	Role     string `json:"role,omitempty"`
}

// RoomInfo represents metadata about a room.
type RoomInfo struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type,omitempty"`
	OwnerID   string    `json:"owner_id,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
	Members   []User    `json:"members"`
}

// SignalType defines signaling message types.
type SignalType string

const (
	SignalOffer           SignalType = "offer"
	SignalAnswer          SignalType = "answer"
	SignalCandidate       SignalType = "candidate"
	SignalSFUOffer        SignalType = "sfu_offer"
	SignalSFUAnswer       SignalType = "sfu_answer"
	SignalSFUCandidate    SignalType = "sfu_candidate"
	SignalSubscribe       SignalType = "subscribe"
	SignalLeave           SignalType = "leave"
	SignalReady           SignalType = "ready"
	SignalKeyExchange     SignalType = "key_exchange"
	SignalGetPreKeyBundle SignalType = "get_prekey_bundle"
	SignalPreKeyRefill    SignalType = "prekey_refill"
	SignalEncryptedFanout SignalType = "encrypted_fanout"
	SignalJoin            SignalType = "join"
	SignalTyping        SignalType = "typing"
	SignalMessage       SignalType = "message"
	SignalMute          SignalType = "mute"
	SignalCameraToggle  SignalType = "camera_toggle"
	SignalScreenStart   SignalType = "screen_share_start"
	SignalScreenStop    SignalType = "screen_share_stop"
	SignalPin           SignalType = "pin"
	SignalCallStart     SignalType = "call_start"
	SignalCallEnd       SignalType = "call_end"
	SignalSync          SignalType = "message_sync"
	SignalSyncRequest   SignalType = "sync_request"
	SignalICERestart    SignalType = "ice_restart"
	SignalNetworkChange SignalType = "network_change"
	SignalActiveSpeaker SignalType = "active_speaker"
	SignalTrackAdded    SignalType = "track_added"
	SignalError         SignalType = "error"
)

// Signal represents a WebRTC signaling message.
type Signal struct {
	Type      SignalType  `json:"type"`
	From      string      `json:"from"`
	To        string      `json:"to,omitempty"`
	RoomID    string      `json:"room_id,omitempty"`
	Payload   []byte      `json:"payload,omitempty"`
	SDP       string      `json:"sdp,omitempty"`
	Candidate interface{} `json:"candidate,omitempty"`
	Ready     bool        `json:"ready,omitempty"`
	AckID     string      `json:"ack_id,omitempty"`
	Timestamp time.Time   `json:"timestamp"`
}

// CallType defines the type of call.
type CallType string

const (
	CallAudio CallType = "audio"
	CallVideo CallType = "video"
)

// CallEvent represents a call lifecycle event.
type CallEvent struct {
	Status  string    `json:"status,omitempty"` // "started" or "ended"
	Type    CallType  `json:"type"`
	RoomID  string    `json:"room_id"`
	Caller  string    `json:"caller"`
	Callee  string    `json:"callee,omitempty"`
	Started time.Time `json:"started"`
	Ended   time.Time `json:"ended,omitempty"`
}

// PresenceEvent represents an online/offline presence notification.
type PresenceEvent struct {
	Type     string `json:"type"` // "presence"
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	RoomID   string `json:"room_id"`
	Online   bool   `json:"online"`
}

// AckMessage is sent back to confirm message receipt.
type AckMessage struct {
	Type  string `json:"type"` // "ack"
	AckID string `json:"ack_id"`
	MsgID string `json:"msg_id"`
}

// Notification represents a server-to-client alert or background notification.
type Notification struct {
	Type      string                 `json:"type"` // "notification"
	ID        string                 `json:"id"`
	Title     string                 `json:"title"`
	Body      string                 `json:"body"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}

// EncryptedFanout represents a fan-out message sent to multiple devices.
// Used for "Signal-grade" multi-device E2EE (Sesame lite).
type EncryptedFanout struct {
	Type     string            `json:"type"` // "encrypted_fanout"
	SenderID string            `json:"sender_id"`
	Payloads map[string][]byte `json:"payloads"` // map[deviceID]encryptedPayload
}
