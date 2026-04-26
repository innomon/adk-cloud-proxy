package adk

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"google.golang.org/genai"
)

// ADKContentToADKMessage converts an ADK genai.Content into a ADK message.
func ADKContentToADKMessage(content *genai.Content) *ADKMessage {
	role := "user"
	if content.Role == "model" {
		role = "assistant"
	}

	var parts []MessageContent
	for _, part := range content.Parts {
		if part.Text != "" {
			parts = append(parts, MessageContent{
				Type: "text",
				Text: part.Text,
			})
		}
		if part.FunctionCall != nil {
			parts = append(parts, MessageContent{
				Type: "toolRequest",
				ID:   part.FunctionCall.ID,
				ToolCall: &ToolCall{
					Name:      part.FunctionCall.Name,
					Arguments: part.FunctionCall.Args,
				},
			})
		}
		if part.FunctionResponse != nil {
			respText, _ := json.Marshal(part.FunctionResponse.Response)
			parts = append(parts, MessageContent{
				Type: "toolResponse",
				ID:   part.FunctionResponse.ID,
				ToolResult: &ToolResult{
					Content: []MessageContent{
						{Type: "text", Text: string(respText)},
					},
					IsError: false,
				},
			})
		}
		if part.InlineData != nil {
			parts = append(parts, MessageContent{
				Type:     "image",
				Data:     base64.StdEncoding.EncodeToString(part.InlineData.Data),
				MimeType: part.InlineData.MIMEType,
			})
		}
	}

	return &ADKMessage{
		Role:    role,
		Created: time.Now().Unix(),
		Content: parts,
		Metadata: &MessageMetadata{
			UserVisible:  true,
			AgentVisible: true,
		},
	}
}

// ADKRunSSERequestToReplyRequest converts a session ID and ADK content into a
// ADK ReplyRequest suitable for the streaming reply endpoint.
func ADKRunSSERequestToReplyRequest(sessionID string, content *genai.Content) *ReplyRequest {
	msg := ADKContentToADKMessage(content)
	return &ReplyRequest{
		UserMessage: msg,
		SessionID:   sessionID,
	}
}

// ADKEvent represents an event in the ADK REST API SSE stream.
type ADKEvent struct {
	ID            string                                      `json:"id"`
	Time          int64                                       `json:"time"`
	InvocationID  string                                      `json:"invocationId"`
	Branch        string                                      `json:"branch"`
	Author        string                                      `json:"author"`
	Partial       bool                                        `json:"partial"`
	Content       *genai.Content                              `json:"content,omitempty"`
	TurnComplete  bool                                        `json:"turnComplete"`
	Interrupted   bool                                        `json:"interrupted"`
	ErrorCode     string                                      `json:"errorCode,omitempty"`
	ErrorMessage  string                                      `json:"errorMessage,omitempty"`
	Actions       *ADKEventActions                            `json:"actions,omitempty"`
	UsageMetadata *genai.GenerateContentResponseUsageMetadata `json:"usageMetadata,omitempty"`
}

// ADKEventActions holds state changes associated with an ADK event.
type ADKEventActions struct {
	StateDelta map[string]any `json:"stateDelta,omitempty"`
}

// ADKSSEEventToADKEvent converts a ADK SSE event into an ADK REST event.
func ADKSSEEventToADKEvent(sse *SSEEvent, invocationID string) (*ADKEvent, error) {
	switch sse.Type {
	case "Message":
		content := ADKMessageToADKContent(sse.Message)
		return &ADKEvent{
			ID:           fmt.Sprintf("evt_%d", time.Now().UnixNano()),
			Time:         time.Now().Unix(),
			InvocationID: invocationID,
			Author:       "goose",
			Content:      content,
		}, nil

	case "Finish":
		evt := &ADKEvent{
			ID:           fmt.Sprintf("evt_%d", time.Now().UnixNano()),
			Time:         time.Now().Unix(),
			InvocationID: invocationID,
			Author:       "goose",
			TurnComplete: true,
		}
		if sse.TokenState != nil {
			evt.UsageMetadata = ADKTokenStateToUsageMetadata(sse.TokenState)
		}
		return evt, nil

	case "Error":
		return &ADKEvent{
			ID:           fmt.Sprintf("evt_%d", time.Now().UnixNano()),
			Time:         time.Now().Unix(),
			InvocationID: invocationID,
			Author:       "goose",
			ErrorCode:    "GOOSE_ERROR",
			ErrorMessage: sse.Error,
		}, nil

	case "Ping":
		return nil, nil

	default:
		return nil, nil
	}
}

// ADKMessageToADKContent converts a ADK message into a genai Content.
func ADKMessageToADKContent(msg *ADKMessage) *genai.Content {
	role := msg.Role
	if role == "assistant" {
		role = "model"
	}

	var parts []*genai.Part
	for _, mc := range msg.Content {
		switch mc.Type {
		case "text":
			parts = append(parts, genai.NewPartFromText(mc.Text))

		case "toolRequest":
			part := &genai.Part{
				FunctionCall: &genai.FunctionCall{
					ID:   mc.ID,
					Name: mc.ToolCall.Name,
					Args: mc.ToolCall.Arguments,
				},
			}
			parts = append(parts, part)

		case "toolResponse":
			resultText := extractToolResultText(mc.ToolResult)
			part := &genai.Part{
				FunctionResponse: &genai.FunctionResponse{
					ID:       mc.ID,
					Name:     "",
					Response: map[string]any{"result": resultText},
				},
			}
			parts = append(parts, part)

		case "thinking", "reasoning":
			text := mc.Thinking
			if text == "" {
				text = mc.Text
			}
			part := genai.NewPartFromText(text)
			part.Thought = true
			parts = append(parts, part)
		}
	}

	return &genai.Content{Parts: parts, Role: role}
}

// ADKTokenStateToUsageMetadata converts ADK token state into genai usage metadata.
func ADKTokenStateToUsageMetadata(ts *TokenState) *genai.GenerateContentResponseUsageMetadata {
	return &genai.GenerateContentResponseUsageMetadata{
		PromptTokenCount:     ts.InputTokens,
		CandidatesTokenCount: ts.OutputTokens,
		TotalTokenCount:      ts.TotalTokens,
	}
}

// extractToolResultText extracts a text representation from a ToolResult.
func extractToolResultText(tr *ToolResult) string {
	if tr == nil {
		return ""
	}
	for _, c := range tr.Content {
		if c.Type == "text" && c.Text != "" {
			return c.Text
		}
	}
	if tr.StructuredContent != nil {
		b, err := json.Marshal(tr.StructuredContent)
		if err == nil {
			return string(b)
		}
	}
	return ""
}

// ADKToolToADKToolInfo converts an ADK tool declaration to a description string
// suitable for logging/display. ADK manages its own tools via extensions,
// so this is primarily informational.
func ADKToolToADKToolInfo(decl *genai.FunctionDeclaration) map[string]any {
	info := map[string]any{
		"name":        decl.Name,
		"description": decl.Description,
	}
	if decl.Parameters != nil {
		info["parameters"] = decl.Parameters
	}
	return info
}

// ADKToolCallToADKFunctionCall converts a ADK ToolCall to an ADK FunctionCall.
func ADKToolCallToADKFunctionCall(id string, tc *ToolCall) *genai.FunctionCall {
	return &genai.FunctionCall{
		ID:   id,
		Name: tc.Name,
		Args: tc.Arguments,
	}
}

// ADKFunctionResponseToADKToolResult converts an ADK FunctionResponse to a ADK ToolResult.
func ADKFunctionResponseToADKToolResult(fr *genai.FunctionResponse) *ToolResult {
	text := ""
	if fr.Response != nil {
		data, err := json.Marshal(fr.Response)
		if err == nil {
			text = string(data)
		}
	}
	return &ToolResult{
		Content: []MessageContent{
			{Type: "text", Text: text},
		},
		IsError: false,
	}
}
