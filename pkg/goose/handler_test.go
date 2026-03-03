package goose

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandler_GetSession(t *testing.T) {
	sm := NewSessionManager(nil, ".")
	// Manually add a session to the manager since we don't have a real client.
	sm.mu.Lock()
	sm.adkToGoose["test-session"] = "goose-id"
	sm.gooseToADK["goose-id"] = "test-session"
	sm.mu.Unlock()

	h := NewHandler(sm, nil)

	req := httptest.NewRequest("GET", "/apps/test-app/users/test-user/sessions/test-session", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status OK, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp["id"] != "test-session" {
		t.Errorf("expected id %q, got %q", "test-session", resp["id"])
	}
	if resp["appName"] != "test-app" {
		t.Errorf("expected appName %q, got %q", "test-app", resp["appName"])
	}
	if resp["userId"] != "test-user" {
		t.Errorf("expected userId %q, got %q", "test-user", resp["userId"])
	}
}

func TestHandler_GetSession_NotFound(t *testing.T) {
	sm := NewSessionManager(nil, ".")
	h := NewHandler(sm, nil)

	req := httptest.NewRequest("GET", "/apps/test-app/users/test-user/sessions/non-existent", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status NotFound, got %d", w.Code)
	}
}
