package cluster

import (
	"context"
	"io"

	"github.com/nats-io/nats.go"
)

type NATSBackend struct {
	conn    *nats.Conn
	subject string
}

func NewNATSBackend(url, subject string) (*NATSBackend, error) {
	if subject == "" {
		subject = "mana.cluster"
	}
	conn, err := nats.Connect(url)
	if err != nil {
		return nil, err
	}
	return &NATSBackend{conn: conn, subject: subject}, nil
}

func (b *NATSBackend) Kind() string { return "nats" }

func (b *NATSBackend) Publish(_ context.Context, event Event) error {
	data, err := encodeEvent(event)
	if err != nil {
		return err
	}
	return b.conn.Publish(b.subject, data)
}

func (b *NATSBackend) Subscribe(handler func(Event)) (io.Closer, error) {
	sub, err := b.conn.Subscribe(b.subject, func(msg *nats.Msg) {
		event, err := decodeEvent(msg.Data)
		if err != nil {
			return
		}
		handler(event)
	})
	if err != nil {
		return nil, err
	}
	return closerFunc(func() error { return sub.Unsubscribe() }), nil
}

func (b *NATSBackend) Close() error {
	b.conn.Drain()
	b.conn.Close()
	return nil
}
