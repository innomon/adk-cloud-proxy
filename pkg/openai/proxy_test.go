package openai

import (
	"encoding/json"
	"testing"
)

func TestConvertToADKContent(t *testing.T) {
	p := &Proxy{}
	messages := []Message{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "Hello!"},
	}

	adkContent := p.convertToADKContent(messages)

	if adkContent.Role != "user" {
		t.Errorf("expected role user, got %s", adkContent.Role)
	}

	if len(adkContent.Parts) != 2 {
		t.Errorf("expected 2 parts, got %d", len(adkContent.Parts))
	}

	if adkContent.Parts[0].Text != "[System]: You are a helpful assistant." {
		t.Errorf("unexpected part 0: %s", adkContent.Parts[0].Text)
	}

	if adkContent.Parts[1].Text != "Hello!" {
		t.Errorf("unexpected part 1: %s", adkContent.Parts[1].Text)
	}
}

func TestJSONMarshaling(t *testing.T) {
	reqJSON := `{"model": "gpt-3.5-turbo", "messages": [{"role": "user", "content": "Hello"}], "stream": true}`
	var req ChatCompletionRequest
	if err := json.Unmarshal([]byte(reqJSON), &req); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if req.Model != "gpt-3.5-turbo" {
		t.Errorf("expected model gpt-3.5-turbo, got %s", req.Model)
	}

	if !req.Stream {
		t.Error("expected stream true")
	}
}

func TestADKContentToOpenAIMessages(t *testing.T) {
	content := ADKContent{
		Parts: []ADKPart{
			{Text: "[System]: Instructions"},
			{Text: "User message"},
			{Text: "[Assistant]: Reply"},
		},
	}

	messages := ADKContentToOpenAIMessages(content)

	if len(messages) != 3 {
		t.Errorf("expected 3 messages, got %d", len(messages))
	}

	if messages[0].Role != "system" || messages[0].Content != "Instructions" {
		t.Errorf("unexpected message 0: %+v", messages[0])
	}
	if messages[1].Role != "user" || messages[1].Content != "User message" {
		t.Errorf("unexpected message 1: %+v", messages[1])
	}
	if messages[2].Role != "assistant" || messages[2].Content != "Reply" {
		t.Errorf("unexpected message 2: %+v", messages[2])
	}
}

func TestOpenAIChunkToADKEvent(t *testing.T) {
	chunk := &ChatCompletionChunk{
		Choices: []ChunkChoice{{
			Delta: Message{Content: "Hello world"},
		}},
	}

	event := OpenAIChunkToADKEvent(chunk)

	if event == nil {
		t.Fatal("expected event, got nil")
	}

	if event.Author != "assistant" {
		t.Errorf("expected author assistant, got %s", event.Author)
	}

	if len(event.Content.Parts) != 1 || event.Content.Parts[0].Text != "Hello world" {
		t.Errorf("unexpected event content: %+v", event.Content)
	}
}
