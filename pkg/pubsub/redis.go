package pubsub

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/go-redis/redis/v8"
)

type redisPubSub struct {
	client *redis.Client
}

func init() {
	Register("redis", func(config map[string]interface{}) (PubSub, error) {
		addr, ok := config["address"].(string)
		if !ok {
			return nil, fmt.Errorf("redis address required")
		}
		password, _ := config["password"].(string)
		db, _ := config["db"].(float64) // JSON unmarshal uses float64 for numbers

		client := redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: password,
			DB:       int(db),
		})

		return &redisPubSub{client: client}, nil
	})
}

func (r *redisPubSub) Publish(ctx context.Context, subject string, payload []byte) error {
	return r.client.Publish(ctx, subject, payload).Err()
}

func (r *redisPubSub) Subscribe(ctx context.Context, subject string, handler Handler) error {
	pubsub := r.client.Subscribe(ctx, subject)
	go func() {
		ch := pubsub.Channel()
		for {
			select {
			case msg, ok := <-ch:
				if !ok {
					slog.Warn("Redis subscription channel closed", "subject", subject)
					return
				}
				handler(&Message{
					Subject: msg.Channel,
					Payload: []byte(msg.Payload),
				})
			case <-ctx.Done():
				pubsub.Close()
				return
			}
		}
	}()
	return nil
}

func (r *redisPubSub) Close() error {
	return r.client.Close()
}
