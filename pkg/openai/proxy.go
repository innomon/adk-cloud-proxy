package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/innomon/adk-cloud-proxy/pkg/auth"
	"github.com/innomon/adk-cloud-proxy/pkg/config"
)

// Proxy handles OpenAI-compatible requests and translates them to ADK.
type Proxy struct {
	cfg        *config.Config
	issuerSeed []byte
	client     *http.Client
	validator  APIKeyValidator
}

// NewProxy creates a new OpenAI proxy.
func NewProxy(cfg *config.Config, issuerSeed []byte, validator APIKeyValidator) *Proxy {
	return &Proxy{
		cfg:        cfg,
		issuerSeed: issuerSeed,
		client:     &http.Client{Timeout: 5 * time.Minute},
		validator:  validator,
	}
}

// HandleChatCompletions handles POST /v1/chat/completions.
func (p *Proxy) HandleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		p.handleCORS(w)
		return
	}

	if !p.authenticate(r) {
		p.writeError(w, http.StatusUnauthorized, "invalid_api_key", "Incorrect API key provided")
		return
	}

	if r.Method != http.MethodPost {
		p.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST is allowed")
		return
	}

	var req ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		p.writeError(w, http.StatusBadRequest, "invalid_request", fmt.Sprintf("Invalid request: %v", err))
		return
	}

	appID := req.Model
	if appID == "" {
		appID = p.cfg.OpenAI.DefaultAppID
	}
	userID := p.cfg.OpenAI.DefaultUserID
	if userID == "" {
		userID = "openai-user"
	}

	// 1. Create ADK Session
	sessionID, err := p.createADKSession(r.Context(), appID, userID)
	if err != nil {
		p.writeError(w, http.StatusInternalServerError, "session_error", fmt.Sprintf("Failed to create ADK session: %v", err))
		return
	}

	// 2. Convert OpenAI messages to ADK content
	adkContent := p.convertToADKContent(req.Messages)

	// 3. Run ADK SSE
	resp, err := p.runADK(r.Context(), appID, userID, sessionID, adkContent)
	if err != nil {
		p.writeError(w, http.StatusInternalServerError, "adk_error", fmt.Sprintf("Failed to run ADK: %v", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		p.writeError(w, resp.StatusCode, "adk_error", fmt.Sprintf("ADK error: %s", string(body)))
		return
	}

	id := fmt.Sprintf("chatcmpl-%d", rand.Intn(999999999))

	if req.Stream {
		p.handleStreamingResponse(w, resp.Body, id, appID)
	} else {
		p.handleNonStreamingResponse(w, resp.Body, id, appID)
	}
}

func (p *Proxy) createADKSession(ctx context.Context, appID, userID string) (string, error) {
	token, err := auth.GenerateToken(p.issuerSeed, userID, appID, "", 1*time.Hour)
	if err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}

	// Internal request to the local router-proxy server
	url := fmt.Sprintf("http://localhost:8080/apps/%s/users/%s/sessions", appID, userID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-App-ID", appID)

	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var session ADKSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return "", err
	}
	return session.ID, nil
}

func (p *Proxy) convertToADKContent(messages []Message) ADKContent {
	var parts []ADKPart
	for _, msg := range messages {
		content := ""
		switch c := msg.Content.(type) {
		case string:
			content = c
		case []interface{}:
			for _, item := range c {
				if m, ok := item.(map[string]interface{}); ok {
					if t, ok := m["text"].(string); ok {
						content += t
					}
				}
			}
		}

		if content != "" {
			prefix := ""
			switch msg.Role {
			case "system":
				prefix = "[System]: "
			case "assistant":
				prefix = "[Assistant]: "
			}
			parts = append(parts, ADKPart{Text: prefix + content})
		}
	}

	return ADKContent{
		Role:  "user",
		Parts: parts,
	}
}

func (p *Proxy) runADK(ctx context.Context, appID, userID, sessionID string, content ADKContent) (*http.Response, error) {
	token, err := auth.GenerateToken(p.issuerSeed, userID, appID, sessionID, 1*time.Hour)
	if err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}

	adkReq := ADKRunRequest{NewMessage: content}
	body, _ := json.Marshal(adkReq)

	url := fmt.Sprintf("http://localhost:8080/apps/%s/users/%s/sessions/%s/run_sse", appID, userID, sessionID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-App-ID", appID)
	req.Header.Set("Content-Type", "application/json")

	return p.client.Do(req)
}

func (p *Proxy) handleStreamingResponse(w http.ResponseWriter, body io.Reader, id, model string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		p.writeError(w, http.StatusInternalServerError, "streaming_error", "Streaming not supported")
		return
	}

	scanner := bufio.NewScanner(body)
	sentRole := false

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "" {
			continue
		}

		var event ADKEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		if event.ErrorCode != "" {
			p.writeSSEChunk(w, flusher, ChatCompletionChunk{
				ID:      id,
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   model,
				Choices: []ChunkChoice{{
					Index: 0,
					Delta: Message{Content: fmt.Sprintf("[Error: %s]", event.ErrorMessage)},
				}},
			})
			continue
		}

		if event.Author == "user" {
			continue
		}

		if event.Content != nil {
			for _, part := range event.Content.Parts {
				if part.Text == "" {
					continue
				}

				delta := Message{Content: part.Text}
				if !sentRole {
					delta.Role = "assistant"
					sentRole = true
				}

				p.writeSSEChunk(w, flusher, ChatCompletionChunk{
					ID:      id,
					Object:  "chat.completion.chunk",
					Created: time.Now().Unix(),
					Model:   model,
					Choices: []ChunkChoice{{
						Index: 0,
						Delta: delta,
					}},
				})
			}
		}

		if event.TurnComplete {
			finishReason := "stop"
			p.writeSSEChunk(w, flusher, ChatCompletionChunk{
				ID:      id,
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   model,
				Choices: []ChunkChoice{{
					Index:        0,
					FinishReason: &finishReason,
				}},
			})
		}
	}

	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func (p *Proxy) handleNonStreamingResponse(w http.ResponseWriter, body io.Reader, id, model string) {
	scanner := bufio.NewScanner(body)
	var fullContent strings.Builder

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		var event ADKEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		if event.Author == "user" {
			continue
		}

		if event.Content != nil {
			for _, part := range event.Content.Parts {
				fullContent.WriteString(part.Text)
			}
		}
	}

	finishReason := "stop"
	response := ChatCompletion{
		ID:      id,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []Choice{{
			Index: 0,
			Message: Message{
				Role:    "assistant",
				Content: fullContent.String(),
			},
			FinishReason: &finishReason,
		}},
		Usage: Usage{
			PromptTokens:     100, // Estimated
			CompletionTokens: len(fullContent.String()) / 4,
			TotalTokens:      100 + (len(fullContent.String()) / 4),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(response)
}

func (p *Proxy) HandleModels(w http.ResponseWriter, r *http.Request) {
	p.handleCORS(w)
	if r.Method == http.MethodOptions {
		return
	}

	if !p.authenticate(r) {
		p.writeError(w, http.StatusUnauthorized, "invalid_api_key", "Incorrect API key provided")
		return
	}

	response := map[string]any{
		"object": "list",
		"data": []map[string]any{
			{
				"id":      p.cfg.OpenAI.DefaultAppID,
				"object":  "model",
				"created": time.Now().Unix(),
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (p *Proxy) writeSSEChunk(w http.ResponseWriter, flusher http.Flusher, chunk ChatCompletionChunk) {
	data, _ := json.Marshal(chunk)
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

func (p *Proxy) handleCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
}

func (p *Proxy) writeError(w http.ResponseWriter, status int, code, message string) {
	slog.Error("openai proxy error", "status", status, "code", code, "message", message)
	w.Header().Set("Content-Type", "application/json")
	p.handleCORS(w)
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ErrorResponse{
		Error: ErrorDetail{
			Message: message,
			Type:    "error",
			Code:    code,
		},
	})
}

// ADK to OpenAI Translation Helpers (for Connector)

// ADKContentToOpenAIMessages converts ADK content to OpenAI messages.
func ADKContentToOpenAIMessages(content ADKContent) []Message {
	var messages []Message
	for _, part := range content.Parts {
		if part.Text == "" {
			continue
		}
		role := "user"
		text := part.Text
		if strings.HasPrefix(text, "[System]: ") {
			role = "system"
			text = strings.TrimPrefix(text, "[System]: ")
		} else if strings.HasPrefix(text, "[Assistant]: ") {
			role = "assistant"
			text = strings.TrimPrefix(text, "[Assistant]: ")
		}
		messages = append(messages, Message{Role: role, Content: text})
	}
	return messages
}

// OpenAIChunkToADKEvent converts an OpenAI chunk to an ADK event.
func OpenAIChunkToADKEvent(chunk *ChatCompletionChunk) *ADKEvent {
	if len(chunk.Choices) == 0 {
		return nil
	}
	choice := chunk.Choices[0]
	content := choice.Delta.Content
	if content == nil {
		return nil
	}
	text, ok := content.(string)
	if !ok || text == "" {
		return nil
	}

	return &ADKEvent{
		Author: "assistant",
		Content: &ADKEventContent{
			Parts: []ADKPart{{Text: text}},
		},
	}
}

func (p *Proxy) authenticate(r *http.Request) bool {
	if p.validator == nil {
		return true
	}

	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		// Try to validate empty key if no header
		return p.validator.Validate("")
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")
	return p.validator.Validate(token)
}
