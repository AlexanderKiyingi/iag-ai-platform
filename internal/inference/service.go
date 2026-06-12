// Package inference orchestrates the provider call, prompt rendering, and
// usage logging that sit between the HTTP handlers and the LLM provider.
package inference

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"iag-ai-platform/backend/internal/provider"
	"iag-ai-platform/backend/internal/repository"
)

type Service struct {
	provider provider.Provider
	repo     *repository.Repository
	embedDim int
}

func New(p provider.Provider, repo *repository.Repository, embedDim int) *Service {
	return &Service{provider: p, repo: repo, embedDim: embedDim}
}

// ProviderName exposes the active provider for health/overview responses.
func (s *Service) ProviderName() string { return s.provider.Name() }

// DefaultModel exposes the provider's default model.
func (s *Service) DefaultModel() string { return s.provider.DefaultModel() }

// CompletionInput is a direct completion request from a caller.
type CompletionInput struct {
	Model       string
	System      string
	Messages    []provider.Message
	MaxTokens   int
	Temperature float64
}

// Complete runs a completion and records usage.
func (s *Service) Complete(ctx context.Context, in CompletionInput, caller string) (provider.CompletionResult, error) {
	return s.run(ctx, "completion", "", caller, provider.CompletionRequest{
		Model:       in.Model,
		System:      in.System,
		Messages:    in.Messages,
		MaxTokens:   in.MaxTokens,
		Temperature: in.Temperature,
	})
}

// RunPrompt renders a saved prompt template with vars and runs it. modelOverride
// (optional) wins over the prompt's stored model.
func (s *Service) RunPrompt(ctx context.Context, name string, vars map[string]string, modelOverride string, caller string) (provider.CompletionResult, *repository.Prompt, error) {
	p, err := s.repo.GetPrompt(ctx, name)
	if err != nil {
		return provider.CompletionResult{}, nil, err
	}
	model := modelOverride
	if model == "" {
		model = p.Model
	}
	res, err := s.run(ctx, "prompt", name, caller, provider.CompletionRequest{
		Model:    model,
		System:   p.System,
		Messages: []provider.Message{{Role: provider.RoleUser, Content: render(p.Template, vars)}},
	})
	return res, p, err
}

// run is the shared completion path: call the provider, time it, and log usage
// (best-effort) regardless of success.
func (s *Service) run(ctx context.Context, kind, promptName, caller string, req provider.CompletionRequest) (provider.CompletionResult, error) {
	start := time.Now()
	res, err := s.provider.Complete(ctx, req)
	latency := int(time.Since(start).Milliseconds())

	entry := repository.RequestLog{
		Caller:       caller,
		Kind:         kind,
		PromptName:   promptName,
		Provider:     s.provider.Name(),
		Model:        firstNonEmpty(res.Model, req.Model, s.provider.DefaultModel()),
		InputTokens:  res.Usage.InputTokens,
		OutputTokens: res.Usage.OutputTokens,
		LatencyMS:    latency,
		Status:       "ok",
	}
	if err != nil {
		entry.Status = "error"
		entry.Error = err.Error()
	}
	if logErr := s.repo.LogRequest(ctx, entry); logErr != nil {
		slog.WarnContext(ctx, "ai usage log failed", "kind", kind, "caller", caller, "error", logErr)
	}
	return res, err
}

// EmbedResult is the embeddings response.
type EmbedResult struct {
	Vectors [][]float64
	Model   string
	Dim     int
}

// Embed returns deterministic embeddings for the inputs and records usage.
func (s *Service) Embed(ctx context.Context, inputs []string, caller string) (EmbedResult, error) {
	start := time.Now()
	vecs := provider.Embed(inputs, s.embedDim)
	dim := s.embedDim
	if len(vecs) > 0 {
		dim = len(vecs[0])
	}
	tokens := 0
	for _, in := range inputs {
		tokens += len(strings.Fields(in))
	}
	_ = s.repo.LogRequest(ctx, repository.RequestLog{
		Caller:      caller,
		Kind:        "embedding",
		Provider:    "builtin",
		Model:       provider.EmbedModel,
		InputTokens: tokens,
		LatencyMS:   int(time.Since(start).Milliseconds()),
		Status:      "ok",
	})
	return EmbedResult{Vectors: vecs, Model: provider.EmbedModel, Dim: dim}, nil
}

// render substitutes {{var}} placeholders in the template with vars values.
// Unknown placeholders are left intact so missing vars are visible, not silent.
func render(template string, vars map[string]string) string {
	if len(vars) == 0 {
		return template
	}
	out := template
	for k, v := range vars {
		out = strings.ReplaceAll(out, "{{"+k+"}}", v)
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
