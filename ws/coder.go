package ws

import (
	"context"
	"fmt"
	"net/http"

	"github.com/coder/websocket"
)

// CoderConn wraps github.com/coder/websocket.Conn to implement the Conn interface.
type CoderConn struct {
	conn *websocket.Conn
}

// NewCoderConn creates a new CoderConn from a coder/websocket connection.
func NewCoderConn(conn *websocket.Conn) *CoderConn {
	return &CoderConn{conn: conn}
}

// SetReadLimit sets the maximum message size that can be read.
// Messages exceeding this limit will cause a read error.
func (c *CoderConn) SetReadLimit(limit int64) {
	c.conn.SetReadLimit(limit)
}

func (c *CoderConn) Read(ctx context.Context) ([]byte, error) {
	_, data, err := c.conn.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("coder ws read: %w", err)
	}
	return data, nil
}

func (c *CoderConn) Write(ctx context.Context, data []byte) error {
	err := c.conn.Write(ctx, websocket.MessageText, data)
	if err != nil {
		return fmt.Errorf("coder ws write: %w", err)
	}
	return nil
}

func (c *CoderConn) Close() error {
	return c.conn.Close(websocket.StatusNormalClosure, "closing")
}

// CoderDialer implements Dialer using github.com/coder/websocket.
type CoderDialer struct{}

func (d *CoderDialer) Dial(ctx context.Context, url string) (Conn, error) {
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return nil, fmt.Errorf("coder ws dial: %w", err)
	}
	return NewCoderConn(conn), nil
}

// CoderAcceptor upgrades HTTP requests using github.com/coder/websocket.
type CoderAcceptor struct{}

// NewCoderAcceptor creates the default HTTP WebSocket acceptor.
func NewCoderAcceptor() *CoderAcceptor {
	return &CoderAcceptor{}
}

func (a *CoderAcceptor) Accept(w http.ResponseWriter, r *http.Request, cfg AcceptConfig) (Conn, error) {
	opts := &websocket.AcceptOptions{
		Subprotocols: cfg.Subprotocols,
	}

	for _, origin := range cfg.AllowedOrigins {
		if origin == "*" {
			opts.InsecureSkipVerify = true
			conn, err := websocket.Accept(w, r, opts)
			if err != nil {
				return nil, err
			}
			return NewCoderConn(conn), nil
		}
	}

	if len(cfg.AllowedOrigins) > 0 {
		opts.OriginPatterns = cfg.AllowedOrigins
	}

	conn, err := websocket.Accept(w, r, opts)
	if err != nil {
		return nil, err
	}
	return NewCoderConn(conn), nil
}
