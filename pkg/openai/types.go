package openai

// OpenAI Request/Response Types

type Message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type ChatCompletionRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type ChatCompletion struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type ChatCompletionChunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []ChunkChoice `json:"choices"`
	Usage   *Usage        `json:"usage,omitempty"`
}

type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason *string `json:"finish_reason"`
}

type ChunkChoice struct {
	Index        int     `json:"index"`
	Delta        Message `json:"delta"`
	FinishReason *string `json:"finish_reason"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

// ADK Types (matching what router-proxy expects and what target-server provides)

type ADKPart struct {
	Text string `json:"text,omitempty"`
}

type ADKContent struct {
	Role  string    `json:"role"`
	Parts []ADKPart `json:"parts"`
}

type ADKRunRequest struct {
	NewMessage ADKContent `json:"new_message"`
}

type ADKSessionResponse struct {
	ID string `json:"id"`
}

type ADKEvent struct {
	Author       string           `json:"author,omitempty"`
	Content      *ADKEventContent `json:"content,omitempty"`
	ErrorCode    string           `json:"errorCode,omitempty"`
	ErrorMessage string           `json:"errorMessage,omitempty"`
	TurnComplete bool             `json:"turnComplete,omitempty"`
}

type ADKEventContent struct {
	Parts []ADKPart `json:"parts"`
}
