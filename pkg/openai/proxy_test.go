package openai

import (
	"encoding/json"
	"testing"
)

func TestConvertToADKContent(促 *testing.T) {
	p := &Proxy{}
	messages := []Message{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "Hello!"},
	}

	adkContent := p.convertToADKContent(messages)

	if adkContent.Role != "user" {
		促.Errorf("expected role user, got %s", adkContent.Role)
	}

	if len(adkContent.Parts) != 2 {
		促.Errorf("expected 2 parts, got %d", len(adkContent.Parts))
	}

	if adkContent.Parts[0].Text != "[System]: You are a helpful assistant." {
		促.Errorf("unexpected part 0: %s", adkContent.Parts[0].Text)
	}

	if adkContent.Parts[1].Text != "Hello!" {
		促.Errorf("unexpected part 1: %s", adkContent.Parts[1].Text)
	}
}

func TestJSONMarshaling(促 *testing.T) {
	reqJSON := `{"model": "gpt-3.5-turbo", "messages": [{"role": "user", "content": "Hello"}], "stream": true}`
	var req ChatCompletionRequest
	if err := json.Unmarshal([]byte(reqJSON), &req); err != nil {
		促.Fatalf("failed to unmarshal: %v", err)
	}

	if req.Model != "gpt-3.5-turbo" {
		促.Errorf("expected model gpt-3.5-turbo, got %s", req.Model)
	}

	if !req.Stream {
		促.Error("expected stream true")
	}
}
