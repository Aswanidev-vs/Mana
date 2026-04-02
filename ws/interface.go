package ws

import "context"

// Conn represents a WebSocket connection abstraction.
// Implementations must be safe for concurrent use.
type Conn interface {
	// Read reads a message from the connection.
	// It blocks until a message is available or the context is cancelled.
	Read(ctx context.Context) ([]byte, error)

	// Write writes a message to the connection.
	Write(ctx context.Context, data []byte) error

	// Close closes the connection.
	Close() error
}

// MessageType defines WebSocket message types.
type MessageType int

const (
	TextMessage MessageType = iota + 1
	BinaryMessage
)

// Dialer creates outbound WebSocket connections.
type Dialer interface {
	Dial(ctx context.Context, url string) (Conn, error)
}

// ConnHandler is a function that handles new WebSocket connections.
type ConnHandler func(conn Conn)
