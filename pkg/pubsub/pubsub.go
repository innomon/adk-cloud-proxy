package pubsub

import "context"

// Message represents a message exchanged over the pubsub service.
type Message struct {
	Subject string
	Payload []byte
}

// Handler is a function called when a message is received.
type Handler func(msg *Message)

// PubSub defines the interface for a pubsub service.
type PubSub interface {
	// Publish publishes a message to a given subject.
	Publish(ctx context.Context, subject string, payload []byte) error

	// Subscribe subscribes to messages on a given subject.
	Subscribe(ctx context.Context, subject string, handler Handler) error

	// Close closes the pubsub connection.
	Close() error
}

// Registry stores registered PubSub factory functions.
var Registry = make(map[string]func(config map[string]interface{}) (PubSub, error))

// Register adds a new PubSub implementation to the registry.
func Register(name string, factory func(config map[string]interface{}) (PubSub, error)) {
	Registry[name] = factory
}

// New creates a new PubSub instance based on the provided type and configuration.
func New(typeName string, config map[string]interface{}) (PubSub, error) {
	factory, ok := Registry[typeName]
	if !ok {
		return nil, nil
	}
	return factory(config)
}
