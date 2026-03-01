package pubsub

import (
	"context"
	"sync"

	"github.com/nats-io/nats.go"
)

type natsPubSub struct {
	conn *nats.Conn
	subs map[string]*nats.Subscription
	mu   sync.Mutex
}

func init() {
	Register("nats", func(config map[string]interface{}) (PubSub, error) {
		url, ok := config["url"].(string)
		if !ok {
			url = nats.DefaultURL
		}
		conn, err := nats.Connect(url)
		if err != nil {
			return nil, err
		}
		return &natsPubSub{
			conn: conn,
			subs: make(map[string]*nats.Subscription),
		}, nil
	})
}

func (n *natsPubSub) Publish(ctx context.Context, subject string, payload []byte) error {
	return n.conn.Publish(subject, payload)
}

func (n *natsPubSub) Subscribe(ctx context.Context, subject string, handler Handler) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if _, ok := n.subs[subject]; ok {
		return nil // already subscribed
	}

	sub, err := n.conn.Subscribe(subject, func(msg *nats.Msg) {
		handler(&Message{
			Subject: msg.Subject,
			Payload: msg.Data,
		})
	})
	if err != nil {
		return err
	}

	n.subs[subject] = sub
	return nil
}

func (n *natsPubSub) Close() error {
	n.conn.Close()
	return nil
}
