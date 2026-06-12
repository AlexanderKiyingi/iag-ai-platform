package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Anthropic calls the Claude Messages API (https://docs.anthropic.com).
type Anthropic struct {
	apiKey       string
	baseURL      string
	version      string
	defaultModel string
	maxTokens    int
	http         *http.Client
}

// AnthropicConfig wires the Claude provider.
type AnthropicConfig struct {
	APIKey       string
	BaseURL      string // default https://api.anthropic.com
	Version      string // anthropic-version header, default 2023-06-01
	DefaultModel string
	MaxTokens    int
}

func NewAnthropic(cfg AnthropicConfig) *Anthropic {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = "https://api.anthropic.com"
	}
	ver := cfg.Version
	if ver == "" {
		ver = "2023-06-01"
	}
	mt := cfg.MaxTokens
	if mt <= 0 {
		mt = 1024
	}
	return &Anthropic{
		apiKey:       cfg.APIKey,
		baseURL:      base,
		version:      ver,
		defaultModel: cfg.DefaultModel,
		maxTokens:    mt,
		http:         &http.Client{Timeout: 60 * time.Second},
	}
}

func (a *Anthropic) Name() string         { return "anthropic" }
func (a *Anthropic) DefaultModel() string { return a.defaultModel }

type anthropicReq struct {
	Model       string    `json:"model"`
	MaxTokens   int       `json:"max_tokens"`
	System      string    `json:"system,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	Messages    []Message `json:"messages"`
}

type anthropicResp struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Model string `json:"model"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func (a *Anthropic) Complete(ctx context.Context, req CompletionRequest) (CompletionResult, error) {
	model := req.Model
	if model == "" {
		model = a.defaultModel
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = a.maxTokens
	}
	body, err := json.Marshal(anthropicReq{
		Model:       model,
		MaxTokens:   maxTokens,
		System:      req.System,
		Temperature: req.Temperature,
		Messages:    req.Messages,
	})
	if err != nil {
		return CompletionResult{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return CompletionResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", a.apiKey)
	httpReq.Header.Set("anthropic-version", a.version)

	resp, err := a.http.Do(httpReq)
	if err != nil {
		return CompletionResult{}, fmt.Errorf("anthropic request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var out anthropicResp
	_ = json.Unmarshal(raw, &out)
	if resp.StatusCode >= 400 {
		msg := strings.TrimSpace(string(raw))
		if out.Error != nil && out.Error.Message != "" {
			msg = out.Error.Message
		}
		return CompletionResult{}, fmt.Errorf("anthropic %s: %s", resp.Status, msg)
	}

	var sb strings.Builder
	for _, blk := range out.Content {
		if blk.Type == "text" {
			sb.WriteString(blk.Text)
		}
	}
	respModel := out.Model
	if respModel == "" {
		respModel = model
	}
	return CompletionResult{
		Text:     sb.String(),
		Model:    respModel,
		Provider: a.Name(),
		Usage:    Usage{InputTokens: out.Usage.InputTokens, OutputTokens: out.Usage.OutputTokens},
	}, nil
}

// Converse runs one round of a tool-enabled conversation against the Claude
// Messages API, returning text and/or tool_use calls plus the stop_reason.
func (a *Anthropic) Converse(ctx context.Context, req ConverseRequest) (ConverseResult, error) {
	model := req.Model
	if model == "" {
		model = a.defaultModel
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = a.maxTokens
	}

	messages := make([]map[string]any, 0, len(req.Turns))
	for _, t := range req.Turns {
		switch t.Role {
		case RoleAssistant:
			blocks := []map[string]any{}
			if t.Text != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": t.Text})
			}
			for _, tc := range t.ToolCalls {
				var input any
				if len(tc.Input) > 0 {
					_ = json.Unmarshal(tc.Input, &input)
				}
				blocks = append(blocks, map[string]any{"type": "tool_use", "id": tc.ID, "name": tc.Name, "input": input})
			}
			messages = append(messages, map[string]any{"role": "assistant", "content": blocks})
		default: // user
			if len(t.ToolResults) > 0 {
				blocks := make([]map[string]any, 0, len(t.ToolResults))
				for _, tr := range t.ToolResults {
					b := map[string]any{"type": "tool_result", "tool_use_id": tr.CallID, "content": tr.Content}
					if tr.IsError {
						b["is_error"] = true
					}
					blocks = append(blocks, b)
				}
				messages = append(messages, map[string]any{"role": "user", "content": blocks})
			} else {
				messages = append(messages, map[string]any{"role": "user", "content": t.Text})
			}
		}
	}

	payload := map[string]any{"model": model, "max_tokens": maxTokens, "messages": messages}
	if req.System != "" {
		payload["system"] = req.System
	}
	if req.Temperature > 0 {
		payload["temperature"] = req.Temperature
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, ts := range req.Tools {
			schema := ts.InputSchema
			if schema == nil {
				schema = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			tools = append(tools, map[string]any{"name": ts.Name, "description": ts.Description, "input_schema": schema})
		}
		payload["tools"] = tools
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return ConverseResult{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return ConverseResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", a.apiKey)
	httpReq.Header.Set("anthropic-version", a.version)

	resp, err := a.http.Do(httpReq)
	if err != nil {
		return ConverseResult{}, fmt.Errorf("anthropic request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var out struct {
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
		Model      string `json:"model"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &out)
	if resp.StatusCode >= 400 {
		msg := strings.TrimSpace(string(raw))
		if out.Error != nil && out.Error.Message != "" {
			msg = out.Error.Message
		}
		return ConverseResult{}, fmt.Errorf("anthropic %s: %s", resp.Status, msg)
	}

	res := ConverseResult{
		StopReason: out.StopReason,
		Model:      firstNonEmpty(out.Model, model),
		Provider:   a.Name(),
		Usage:      Usage{InputTokens: out.Usage.InputTokens, OutputTokens: out.Usage.OutputTokens},
	}
	var sb strings.Builder
	for _, blk := range out.Content {
		switch blk.Type {
		case "text":
			sb.WriteString(blk.Text)
		case "tool_use":
			res.ToolCalls = append(res.ToolCalls, ToolCall{ID: blk.ID, Name: blk.Name, Input: blk.Input})
		}
	}
	res.Text = sb.String()
	return res, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
