// Package agent runs configured AI agents through a tool-use loop and supports
// multi-agent orchestration: a coordinator agent delegates subtasks to
// specialist agents (via the delegate tool), each of which can call IAG
// microservices (via the call_microservice tool).
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"iag-ai-platform/backend/internal/provider"
	"iag-ai-platform/backend/internal/repository"
	"iag-ai-platform/backend/internal/tools"
)

// Runner executes agents. It is safe for concurrent use.
type Runner struct {
	prov     provider.Provider
	registry *tools.Registry
	repo     *repository.Repository
	maxSteps int
	maxDepth int
}

func NewRunner(prov provider.Provider, registry *tools.Registry, repo *repository.Repository, maxSteps, maxDepth int) *Runner {
	if maxSteps <= 0 {
		maxSteps = 8
	}
	if maxDepth <= 0 {
		maxDepth = 3
	}
	return &Runner{prov: prov, registry: registry, repo: repo, maxSteps: maxSteps, maxDepth: maxDepth}
}

// RunResult is the outcome of an agent run.
type RunResult struct {
	RunID  string         `json:"runId"`
	Agent  string         `json:"agent"`
	Status string         `json:"status"`
	Output string         `json:"output"`
	Steps  int            `json:"steps"`
	Usage  provider.Usage `json:"usage"`
}

// Run executes an agent against a task, looping over tool calls until the model
// produces a final answer or hits the step limit.
func (r *Runner) Run(ctx context.Context, agentName, task string, vars map[string]string, caller string) (*RunResult, error) {
	ag, err := r.repo.GetAgent(ctx, agentName)
	if err != nil {
		return nil, err
	}

	ctx = withCaller(ctx, caller)
	runID, err := r.repo.CreateRun(ctx, ag.Name, caller, task)
	if err != nil {
		return nil, err
	}

	specs := r.registry.Specs(ag.Tools)
	turns := []provider.Turn{{Role: provider.RoleUser, Text: render(task, vars)}}
	var total provider.Usage
	stepIdx := 0
	finalText := ""

	for i := 0; i < r.maxSteps; i++ {
		res, err := r.prov.Converse(ctx, provider.ConverseRequest{
			Model:  ag.Model,
			System: ag.System,
			Turns:  turns,
			Tools:  specs,
		})
		if err != nil {
			_ = r.repo.FinishRun(ctx, runID, "error", err.Error(), stepIdx)
			r.logUsage(ctx, caller, ag, total, "error", err.Error())
			return nil, fmt.Errorf("agent %s converse: %w", ag.Name, err)
		}
		total.InputTokens += res.Usage.InputTokens
		total.OutputTokens += res.Usage.OutputTokens
		finalText = res.Text

		turns = append(turns, provider.Turn{Role: provider.RoleAssistant, Text: res.Text, ToolCalls: res.ToolCalls})
		stepIdx++
		_ = r.repo.AppendStep(ctx, runID, stepIdx, "assistant", "", assistantStepContent(res))

		if res.StopReason != "tool_use" || len(res.ToolCalls) == 0 {
			_ = r.repo.FinishRun(ctx, runID, "ok", finalText, stepIdx)
			r.logUsage(ctx, caller, ag, total, "ok", "")
			return &RunResult{RunID: runID.String(), Agent: ag.Name, Status: "ok", Output: finalText, Steps: stepIdx, Usage: total}, nil
		}

		results := make([]provider.ToolResult, 0, len(res.ToolCalls))
		for _, tc := range res.ToolCalls {
			content, isErr := r.execTool(ctx, tc)
			stepIdx++
			_ = r.repo.AppendStep(ctx, runID, stepIdx, "tool", tc.Name, content)
			results = append(results, provider.ToolResult{CallID: tc.ID, Content: content, IsError: isErr})
		}
		turns = append(turns, provider.Turn{Role: provider.RoleUser, ToolResults: results})
	}

	_ = r.repo.FinishRun(ctx, runID, "max_steps", finalText, stepIdx)
	r.logUsage(ctx, caller, ag, total, "max_steps", "step limit reached")
	return &RunResult{RunID: runID.String(), Agent: ag.Name, Status: "max_steps", Output: finalText, Steps: stepIdx, Usage: total}, nil
}

func (r *Runner) execTool(ctx context.Context, tc provider.ToolCall) (content string, isErr bool) {
	tool, ok := r.registry.Get(tc.Name)
	if !ok {
		return fmt.Sprintf("error: unknown tool %q", tc.Name), true
	}
	out, err := tool.Execute(ctx, tc.Input)
	if err != nil {
		return "error: " + err.Error(), true
	}
	return out, false
}

func (r *Runner) logUsage(ctx context.Context, caller string, ag *repository.Agent, u provider.Usage, status, errMsg string) {
	_ = r.repo.LogRequest(ctx, repository.RequestLog{
		Caller:       caller,
		Kind:         "agent",
		PromptName:   ag.Name,
		Provider:     r.prov.Name(),
		Model:        firstNonEmpty(ag.Model, r.prov.DefaultModel()),
		InputTokens:  u.InputTokens,
		OutputTokens: u.OutputTokens,
		Status:       status,
		Error:        errMsg,
	})
}

// DelegateTool lets a coordinator agent hand a subtask to another agent. It is
// registered into the same registry the runner uses; recursion is bounded by
// maxDepth so coordinators can't loop forever.
func (r *Runner) DelegateTool() tools.Tool {
	return tools.NewFuncTool(
		"delegate",
		"Delegate a subtask to a specialist agent and get its answer. Use this to break a complex task into parts handled by domain experts (e.g. a finance agent, a procurement agent).",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"agent": map[string]any{"type": "string", "description": "Name of the specialist agent to delegate to (see the available agents)."},
				"task":  map[string]any{"type": "string", "description": "The self-contained subtask for that agent."},
			},
			"required": []string{"agent", "task"},
		},
		func(ctx context.Context, input json.RawMessage) (string, error) {
			var in struct {
				Agent string `json:"agent"`
				Task  string `json:"task"`
			}
			if err := json.Unmarshal(input, &in); err != nil {
				return "", fmt.Errorf("invalid input: %w", err)
			}
			if depth(ctx) >= r.maxDepth {
				return "", fmt.Errorf("delegation depth limit (%d) reached; answer directly", r.maxDepth)
			}
			sub, err := r.Run(deeper(ctx), strings.TrimSpace(in.Agent), in.Task, nil, callerFrom(ctx))
			if err != nil {
				return "", err
			}
			return sub.Output, nil
		},
	)
}

func assistantStepContent(res provider.ConverseResult) string {
	var sb strings.Builder
	if res.Text != "" {
		sb.WriteString(res.Text)
	}
	for _, tc := range res.ToolCalls {
		fmt.Fprintf(&sb, "\n[tool_call %s %s]", tc.Name, string(tc.Input))
	}
	return strings.TrimSpace(sb.String())
}

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
