package pubsub

import (
	"context"
	"fmt"
	"log/slog"

	"cloud.google.com/go/pubsub"
)

type gcpPubSub struct {
	client *pubsub.Client
}

func init() {
	Register("gcp", func(config map[string]interface{}) (PubSub, error) {
		projectID, ok := config["project_id"].(string)
		if !ok {
			return nil, fmt.Errorf("gcp project_id required")
		}
		client, err := pubsub.NewClient(context.Background(), projectID)
		if err != nil {
			return nil, err
		}
		return &gcpPubSub{client: client}, nil
	})
}

func (g *gcpPubSub) Publish(ctx context.Context, subject string, payload []byte) error {
	topic := g.client.Topic(subject)
	result := topic.Publish(ctx, &pubsub.Message{Data: payload})
	_, err := result.Get(ctx)
	return err
}

func (g *gcpPubSub) Subscribe(ctx context.Context, subject string, handler Handler) error {
	sub := g.client.Subscription(subject)
	exists, err := sub.Exists(ctx)
	if err != nil {
		return fmt.Errorf("failed to check subscription existence: %w", err)
	}
	if !exists {
		return fmt.Errorf("subscription %q does not exist", subject)
	}

	go func() {
		err := sub.Receive(ctx, func(ctx context.Context, msg *pubsub.Message) {
			handler(&Message{
				Subject: subject,
				Payload: msg.Data,
			})
			msg.Ack()
		})
		if err != nil {
			slog.Error("GCP Subscription error", "subject", subject, "error", err)
		}
	}()
	return nil
}

func (g *gcpPubSub) Close() error {
	return g.client.Close()
}
