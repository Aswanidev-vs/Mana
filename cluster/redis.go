package cluster

import (
	"context"
	"io"

	redis "github.com/redis/go-redis/v9"
)

type RedisBackend struct {
	client  *redis.Client
	channel string
}

func NewRedisBackend(addr, password string, db int, channel string) (*RedisBackend, error) {
	if channel == "" {
		channel = "mana.cluster"
	}
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
	return &RedisBackend{client: client, channel: channel}, nil
}

func (b *RedisBackend) Kind() string { return "redis" }

func (b *RedisBackend) Publish(ctx context.Context, event Event) error {
	data, err := encodeEvent(event)
	if err != nil {
		return err
	}
	return b.client.Publish(ctx, b.channel, data).Err()
}

func (b *RedisBackend) Subscribe(handler func(Event)) (io.Closer, error) {
	pubsub := b.client.Subscribe(context.Background(), b.channel)
	if _, err := pubsub.Receive(context.Background()); err != nil {
		return nil, err
	}

	go func() {
		ch := pubsub.Channel()
		for msg := range ch {
			event, err := decodeEvent([]byte(msg.Payload))
			if err != nil {
				continue
			}
			handler(event)
		}
	}()

	return closerFunc(func() error { return pubsub.Close() }), nil
}

func (b *RedisBackend) Close() error { return b.client.Close() }
