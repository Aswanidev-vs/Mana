package ws

import (
	"context"
	"fmt"
	"sync"
)

// InMemoryConn is an in-process Conn implementation useful for tests and embedded integrations.
type InMemoryConn struct {
	incoming chan []byte
	peer     *InMemoryConn

	mu     sync.RWMutex
	closed bool
	once   sync.Once
}

// NewInMemoryPair creates two connected in-memory WebSocket-like endpoints.
func NewInMemoryPair(buffer int) (*InMemoryConn, *InMemoryConn) {
	if buffer <= 0 {
		buffer = 16
	}

	left := &InMemoryConn{incoming: make(chan []byte, buffer)}
	right := &InMemoryConn{incoming: make(chan []byte, buffer)}
	left.peer = right
	right.peer = left
	return left, right
}

func (c *InMemoryConn) Read(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case data, ok := <-c.incoming:
		if !ok {
			return nil, fmt.Errorf("inmemory conn closed")
		}
		cloned := make([]byte, len(data))
		copy(cloned, data)
		return cloned, nil
	}
}

func (c *InMemoryConn) Write(ctx context.Context, data []byte) error {
	c.mu.RLock()
	closed := c.closed
	peer := c.peer
	c.mu.RUnlock()

	if closed || peer == nil {
		return fmt.Errorf("inmemory conn closed")
	}

	peer.mu.RLock()
	peerClosed := peer.closed
	peer.mu.RUnlock()
	if peerClosed {
		return fmt.Errorf("peer inmemory conn closed")
	}

	cloned := make([]byte, len(data))
	copy(cloned, data)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case peer.incoming <- cloned:
		return nil
	}
}

func (c *InMemoryConn) Close() error {
	c.once.Do(func() {
		c.mu.Lock()
		c.closed = true
		close(c.incoming)
		c.mu.Unlock()
	})
	return nil
}

// InMemoryDialer returns one side of a prebuilt in-memory pair on each Dial call.
type InMemoryDialer struct {
	mu    sync.Mutex
	conns []Conn
}

// NewInMemoryDialer creates a dialer backed by a fixed queue of in-memory connections.
func NewInMemoryDialer(conns ...Conn) *InMemoryDialer {
	return &InMemoryDialer{conns: conns}
}

func (d *InMemoryDialer) Dial(ctx context.Context, url string) (Conn, error) {
	_ = ctx
	_ = url

	d.mu.Lock()
	defer d.mu.Unlock()

	if len(d.conns) == 0 {
		return nil, fmt.Errorf("no inmemory connections available")
	}

	conn := d.conns[0]
	d.conns = d.conns[1:]
	return conn, nil
}
