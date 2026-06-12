// Package provider abstracts the LLM backend behind a small interface so the
// rest of the service is provider-agnostic. The Anthropic (Claude) provider is
// the real implementation; the stub provider gives deterministic, key-free
// responses for local dev and tests.
package provider

import (
	"context"
	"encoding/json"
)

// Role values for chat messages.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Message is one turn in a chat exchange.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// CompletionRequest is a single inference request.
type CompletionRequest struct {
	Model       string    // empty → provider default
	System      string    // optional system prompt
	Messages    []Message // at least one user message
	MaxTokens   int       // 0 → provider default
	Temperature float64   // 0..1
}

// Usage reports token consumption for cost attribution.
type Usage struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
}

// CompletionResult is a completed inference.
type CompletionResult struct {
	Text     string `json:"text"`
	Model    string `json:"model"`
	Provider string `json:"provider"`
	Usage    Usage  `json:"usage"`
}

// ----- Tool-use / multi-turn (agent) types -----

// ToolSpec describes a tool the model may call. InputSchema is a JSON Schema
// object describing the tool's parameters.
type ToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// ToolCall is a model-issued request to invoke a tool.
type ToolCall struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// ToolResult is the outcome of executing a ToolCall, fed back to the model.
type ToolResult struct {
	CallID  string `json:"callId"`
	Content string `json:"content"`
	IsError bool   `json:"isError"`
}

// Turn is one message in an agent conversation. A user turn carries Text or
// ToolResults; an assistant turn carries Text and/or ToolCalls.
type Turn struct {
	Role        string       // RoleUser | RoleAssistant
	Text        string       // free text for this turn
	ToolCalls   []ToolCall   // assistant-issued tool calls
	ToolResults []ToolResult // user-provided results for prior tool calls
}

// ConverseRequest is a multi-turn, tool-enabled completion request.
type ConverseRequest struct {
	Model       string
	System      string
	Turns       []Turn
	Tools       []ToolSpec
	MaxTokens   int
	Temperature float64
}

// ConverseResult is the model's reply: text and/or tool calls, plus why it
// stopped ("tool_use" means it wants tools executed and the loop to continue).
type ConverseResult struct {
	Text       string     `json:"text"`
	ToolCalls  []ToolCall `json:"toolCalls,omitempty"`
	StopReason string     `json:"stopReason"`
	Model      string     `json:"model"`
	Provider   string     `json:"provider"`
	Usage      Usage      `json:"usage"`
}

// Provider is an LLM backend supporting single completions and tool-enabled
// multi-turn conversations (for agents).
type Provider interface {
	// Name is the provider identifier recorded in usage logs.
	Name() string
	// DefaultModel is used when a request omits a model.
	DefaultModel() string
	// Complete runs a single chat completion.
	Complete(ctx context.Context, req CompletionRequest) (CompletionResult, error)
	// Converse runs one round of a tool-enabled conversation. The caller (the
	// agent runner) executes any returned ToolCalls and calls Converse again
	// with the results appended as a new user Turn.
	Converse(ctx context.Context, req ConverseRequest) (ConverseResult, error)
}
