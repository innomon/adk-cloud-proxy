package adk

import (
	"context"
	"fmt"
	"sync"
)

// SessionManager maintains bidirectional mappings between ADK session IDs
// and ADK session IDs, creating ADK sessions on demand.
type SessionManager struct {
	mu         sync.RWMutex
	adkToADK map[string]string // adkSessionID → gooseSessionID
	gooseToADK map[string]string // reverse mapping
	client     *Client
	workingDir string
}

// NewSessionManager creates a SessionManager that uses client to start/stop
// ADK agent sessions rooted at workingDir.
func NewSessionManager(client *Client, workingDir string) *SessionManager {
	return &SessionManager{
		adkToADK: make(map[string]string),
		gooseToADK: make(map[string]string),
		client:     client,
		workingDir: workingDir,
	}
}

// GetOrCreate returns the ADK session ID mapped to adkSessionID, starting a
// new ADK agent session if one does not already exist.
func (sm *SessionManager) GetOrCreate(ctx context.Context, adkSessionID string) (string, error) {
	sm.mu.RLock()
	if gooseID, ok := sm.adkToADK[adkSessionID]; ok {
		sm.mu.RUnlock()
		return gooseID, nil
	}
	sm.mu.RUnlock()

	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Double-check after acquiring write lock.
	if gooseID, ok := sm.adkToADK[adkSessionID]; ok {
		return gooseID, nil
	}

	resp, err := sm.client.StartAgent(ctx, &StartAgentRequest{
		WorkingDir: sm.workingDir,
	})
	if err != nil {
		return "", fmt.Errorf("start goose agent for ADK session %s: %w", adkSessionID, err)
	}

	sm.adkToADK[adkSessionID] = resp.ID
	sm.gooseToADK[resp.ID] = adkSessionID

	return resp.ID, nil
}

// Stop stops the ADK agent session mapped to adkSessionID and removes the
// bidirectional mapping.
func (sm *SessionManager) Stop(ctx context.Context, adkSessionID string) error {
	sm.mu.Lock()
	gooseID, ok := sm.adkToADK[adkSessionID]
	if !ok {
		sm.mu.Unlock()
		return fmt.Errorf("no goose session for ADK session %s", adkSessionID)
	}
	delete(sm.adkToADK, adkSessionID)
	delete(sm.gooseToADK, gooseID)
	sm.mu.Unlock()

	return sm.client.StopAgent(ctx, gooseID)
}

// GetADKSessionID returns the ADK session ID for the given ADK session ID.
func (sm *SessionManager) GetADKSessionID(adkSessionID string) (string, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	gooseID, ok := sm.adkToADK[adkSessionID]
	return gooseID, ok
}

// ListMappedSessions returns a copy of the current ADK-to-ADK session mappings.
func (sm *SessionManager) ListMappedSessions() map[string]string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	out := make(map[string]string, len(sm.adkToADK))
	for k, v := range sm.adkToADK {
		out[k] = v
	}
	return out
}
