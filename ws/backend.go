package ws

import "net/http"

// AcceptConfig controls how a backend upgrades incoming HTTP requests to WebSocket connections.
type AcceptConfig struct {
	AllowedOrigins []string
	Subprotocols   []string
}

// Acceptor upgrades inbound HTTP requests into framework connections.
type Acceptor interface {
	Accept(w http.ResponseWriter, r *http.Request, cfg AcceptConfig) (Conn, error)
}

// ReadLimitConn is implemented by connections that support inbound frame limits.
type ReadLimitConn interface {
	Conn
	SetReadLimit(limit int64)
}
