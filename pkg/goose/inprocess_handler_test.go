package goose

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInProcessADKHandler_Routing(t *testing.T) {
	// This would require a real MultiRunner which requires real agentic config
	// But we can check if the mux is set up correctly.
	mr := &MultiRunner{
		runners: make(map[string]*runner.Runner),
	}
	h := NewInProcessADKHandler(mr)

	req := httptest.NewRequest("POST", "/apps/test-app/users/test-user/sessions", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}
