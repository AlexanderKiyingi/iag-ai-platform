package provider

import (
	"context"
	"fmt"
	"strings"
)

// Stub is a deterministic, key-free provider for local dev and tests. It echoes
// a summary of the request rather than calling any external API, so the full
// request → log → response path can be exercised without an ANTHROPIC_API_KEY.
type Stub struct {
	defaultModel string
}

func NewStub(defaultModel string) *Stub {
	if defaultModel == "" {
		defaultModel = "stub-model"
	}
	return &Stub{defaultModel: defaultModel}
}

func (s *Stub) Name() string         { return "stub" }
func (s *Stub) DefaultModel() string { return s.defaultModel }

func (s *Stub) Complete(_ context.Context, req CompletionRequest) (CompletionResult, error) {
	model := req.Model
	if model == "" {
		model = s.defaultModel
	}
	var lastUser string
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == RoleUser {
			lastUser = req.Messages[i].Content
			break
		}
	}
	text := fmt.Sprintf("[stub:%s] %s", model, summarize(lastUser))
	in := countWords(req.System) + messagesWords(req.Messages)
	return CompletionResult{
		Text:     text,
		Model:    model,
		Provider: s.Name(),
		Usage:    Usage{InputTokens: in, OutputTokens: countWords(text)},
	}, nil
}

// Converse returns a final answer immediately without issuing tool calls, so
// the agent loop terminates. Real multi-agent tool use requires a live model;
// the stub keeps the wiring exercisable without an API key.
func (s *Stub) Converse(_ context.Context, req ConverseRequest) (ConverseResult, error) {
	model := req.Model
	if model == "" {
		model = s.defaultModel
	}
	var lastUser string
	for i := len(req.Turns) - 1; i >= 0; i-- {
		if req.Turns[i].Role == RoleUser && req.Turns[i].Text != "" {
			lastUser = req.Turns[i].Text
			break
		}
	}
	text := fmt.Sprintf("[stub-agent:%s] %s (set ANTHROPIC_API_KEY for real tool-using agents; %d tool(s) available)",
		model, summarize(lastUser), len(req.Tools))
	return ConverseResult{
		Text:       text,
		StopReason: "end_turn",
		Model:      model,
		Provider:   s.Name(),
		Usage:      Usage{InputTokens: countWords(lastUser), OutputTokens: countWords(text)},
	}, nil
}

func summarize(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "(no user message)"
	}
	const max = 160
	if len(s) > max {
		return "Acknowledged: " + s[:max] + "…"
	}
	return "Acknowledged: " + s
}

func messagesWords(msgs []Message) int {
	n := 0
	for _, m := range msgs {
		n += countWords(m.Content)
	}
	return n
}

func countWords(s string) int {
	f := strings.Fields(s)
	return len(f)
}
