package router

import (
	"fmt"
	"sync"

	pb "github.com/innomon/adk-cloud-proxy/pkg/tunnel"
)

// routeKey is the composite key used to look up a connector stream.
type routeKey struct {
	UserID string
	AppID  string
}

// ConnectorStream holds the gRPC stream and a channel-based mechanism to
// correlate requests with responses for a single connector.
type ConnectorStream struct {
	Stream   pb.TunnelService_ConnectServer
	Pending  map[string]chan *pb.TunnelMessage // request_id -> response channel
	mu       sync.Mutex
}

// Registry maintains the mapping from (userid, appid) to active connector streams.
type Registry struct {
	mu      sync.RWMutex
	streams map[routeKey]*ConnectorStream
}

// NewRegistry creates a new empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		streams: make(map[routeKey]*ConnectorStream),
	}
}

// Register adds a connector stream to the registry.
func (r *Registry) Register(userID, appID string, stream pb.TunnelService_ConnectServer) *ConnectorStream {
	key := routeKey{UserID: userID, AppID: appID}
	r.mu.Lock()
	defer r.mu.Unlock()

	// If a stream already exists, we should probably close it to avoid leaks.
	if existing, ok := r.streams[key]; ok {
		existing.CleanupPending()
	}

	cs := &ConnectorStream{
		Stream:  stream,
		Pending: make(map[string]chan *pb.TunnelMessage),
	}
	r.streams[key] = cs
	return cs
}

// Unregister removes a connector stream from the registry.
func (r *Registry) Unregister(userID, appID string) {
	key := routeKey{UserID: userID, AppID: appID}
	r.mu.Lock()
	delete(r.streams, key)
	r.mu.Unlock()
}

// Lookup finds an active connector stream for the given userid and appid.
func (r *Registry) Lookup(userID, appID string) (*ConnectorStream, error) {
	key := routeKey{UserID: userID, AppID: appID}
	r.mu.RLock()
	cs, ok := r.streams[key]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("no connector registered for userid=%q appid=%q", userID, appID)
	}
	return cs, nil
}

// RegisterPending creates a channel to receive the response for a given request ID.
func (cs *ConnectorStream) RegisterPending(requestID string) chan *pb.TunnelMessage {
	ch := make(chan *pb.TunnelMessage, 1)
	cs.mu.Lock()
	cs.Pending[requestID] = ch
	cs.mu.Unlock()
	return ch
}

// ResolvePending delivers a response message to the waiting request handler.
func (cs *ConnectorStream) ResolvePending(requestID string, msg *pb.TunnelMessage) {
	cs.mu.Lock()
	ch, ok := cs.Pending[requestID]
	if ok {
		delete(cs.Pending, requestID)
	}
	cs.mu.Unlock()
	if ok {
		ch <- msg
	}
}

// CleanupPending closes all pending channels (used when a connector disconnects).
func (cs *ConnectorStream) CleanupPending() {
	cs.mu.Lock()
	for id, ch := range cs.Pending {
		close(ch)
		delete(cs.Pending, id)
	}
	cs.mu.Unlock()
}
