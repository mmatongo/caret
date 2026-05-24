// Package common defines common types shared across the backend
package common

import "context"

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message struct {
	Role    Role `json:"role"`
	Content any  `json:"content"`
}

type Part struct {
	Type    string `json:"type"`
	Text    string `json:"text,omitempty"`
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	Input   string `json:"input,omitempty"`
	Content string `json:"content,omitempty"`
	IsError bool   `json:"is_error,omitempty"`
}

type DeltaType string

const (
	DeltaText     DeltaType = "text"
	DeltaToolCall DeltaType = "tool_call"
	DeltaError    DeltaType = "error"
)

type Delta struct {
	Type     DeltaType `json:"type"`
	Text     string    `json:"text,omitempty"`
	ToolCall *ToolCall `json:"toolCall,omitempty"`
	Error    string    `json:"error,omitempty"`
}

type ToolCall struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Input string `json:"input"`
}

type ToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type StreamRequest struct {
	Model        string
	Messages     []Message
	SystemPrompt string
	Tools        []ToolSpec
	MaxTokens    int
}

type Provider interface {
	Stream(ctx context.Context, req StreamRequest) (<-chan Delta, error)
	Models(ctx context.Context) ([]string, error)
}
