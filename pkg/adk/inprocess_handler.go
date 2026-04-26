package adk

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/session"
)

// InProcessADKHandler implements the ADK REST API by calling runner.Runner directly.
type InProcessADKHandler struct {
	multiRunner *MultiRunner
	mux         *http.ServeMux
}

func NewInProcessADKHandler(mr *MultiRunner) *InProcessADKHandler {
	h := &InProcessADKHandler{
		multiRunner: mr,
		mux:         http.NewServeMux(),
	}

	h.mux.HandleFunc("POST /apps/{app}/users/{user}/sessions", h.handleCreateSession)
	h.mux.HandleFunc("GET /apps/{app}/users/{user}/sessions", h.handleListSessions)
	h.mux.HandleFunc("GET /apps/{app}/users/{user}/sessions/{session}", h.handleGetSession)
	h.mux.HandleFunc("POST /apps/{app}/users/{user}/sessions/{session}/run_sse", h.handleRunSSE)
	h.mux.HandleFunc("DELETE /apps/{app}/users/{user}/sessions/{session}", h.handleDeleteSession)

	return h
}

func (h *InProcessADKHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *InProcessADKHandler) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("app")
	user := r.PathValue("user")

	// In-process runner might not need explicit "session creation" if it's handled by sessionService
	// But we need to return a session ID.
	sessionID := fmt.Sprintf("sess_%d", time.Now().UnixNano())

	writeJSON(w, http.StatusOK, map[string]any{
		"id":      sessionID,
		"appName": app,
		"userId":  user,
	})
}

func (h *InProcessADKHandler) handleListSessions(w http.ResponseWriter, r *http.Request) {
	// Not easily implemented without tracking sessions in handler
	writeJSON(w, http.StatusOK, []any{})
}

func (h *InProcessADKHandler) handleGetSession(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("app")
	user := r.PathValue("user")
	sessionID := r.PathValue("session")

	writeJSON(w, http.StatusOK, map[string]any{
		"id":      sessionID,
		"appName": app,
		"userId":  user,
	})
}

func (h *InProcessADKHandler) handleRunSSE(w http.ResponseWriter, r *http.Request) {
	agentName := r.PathValue("app")
	userID := r.PathValue("user")
	sessionID := r.PathValue("session")

	var req RunSSERequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("decode request: %v", err))
		return
	}

	rnr, ok := h.multiRunner.GetRunner(agentName)
	if !ok {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	invocationID := fmt.Sprintf("inv_%d", time.Now().UnixNano())

	// Call Run and iterate over events.
	events := rnr.Run(r.Context(), userID, sessionID, req.NewMessage, agent.RunConfig{
		StreamingMode: agent.StreamingModeSSE,
	})

	for ev, err := range events {
		if err != nil {
			log.Printf("Runner error: %v", err)
			adkEvent := &ADKEvent{
				ID:           fmt.Sprintf("evt_%d", time.Now().UnixNano()),
				Time:         time.Now().Unix(),
				InvocationID: invocationID,
				ErrorCode:    "RUNNER_ERROR",
				ErrorMessage: err.Error(),
			}
			sendSSE(w, flusher, adkEvent)
			return
		}

		adkEvent := translateSessionEventToADKEvent(ev, invocationID)
		if adkEvent != nil {
			sendSSE(w, flusher, adkEvent)
		}
	}
}

func (h *InProcessADKHandler) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func translateSessionEventToADKEvent(ev *session.Event, invocationID string) *ADKEvent {
	ae := &ADKEvent{
		ID:            ev.ID,
		Time:          ev.Timestamp.Unix(),
		InvocationID:  invocationID,
		Branch:        ev.Branch,
		Author:        ev.Author,
		Partial:       ev.Partial,
		Content:       ev.Content,
		TurnComplete:  ev.TurnComplete,
		Interrupted:   ev.Interrupted,
		ErrorCode:     ev.ErrorCode,
		ErrorMessage:  ev.ErrorMessage,
		UsageMetadata: ev.UsageMetadata,
	}

	if len(ev.Actions.StateDelta) > 0 {
		ae.Actions = &ADKEventActions{
			StateDelta: ev.Actions.StateDelta,
		}
	}

	return ae
}

func sendSSE(w http.ResponseWriter, flusher http.Flusher, ev *ADKEvent) {
	jsonBytes, err := json.Marshal(ev)
	if err != nil {
		log.Printf("marshal ADK event: %v", err)
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", jsonBytes)
	flusher.Flush()
}
