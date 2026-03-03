package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /apps/{app}/users/{user}/sessions/{session}", handleGetSession)
	mux.HandleFunc("POST /apps/{app}/users/{user}/sessions/{session}/run_sse", handleRunSSE)
	mux.HandleFunc("/", handleRequest)

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	go func() {
		log.Printf("Target ADK server listening on :%s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server failed: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

func handleGetSession(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("app")
	user := r.PathValue("user")
	session := r.PathValue("session")

	log.Printf("GET Session: app=%s user=%s session=%s", app, user, session)

	resp := map[string]any{
		"id":      session,
		"appName": app,
		"userId":  user,
		"state":   map[string]any{},
		"events":  []any{},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func handleRunSSE(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("app")
	user := r.PathValue("user")
	session := r.PathValue("session")

	log.Printf("POST RunSSE: app=%s user=%s session=%s", app, user, session)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Send a simple mock event.
	event := map[string]any{
		"type": "text",
		"text": fmt.Sprintf("Hello! This is a mock response from target-server for session %s", session),
	}
	data, _ := json.Marshal(event)
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

func handleRequest(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	log.Printf("Request: %s %s body=%s", r.Method, r.URL.Path, string(body))

	resp := map[string]string{
		"message": "Hello from target ADK server",
		"echo":    string(body),
		"path":    r.URL.Path,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
