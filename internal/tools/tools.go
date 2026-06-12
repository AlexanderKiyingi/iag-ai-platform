// Package tools defines the capabilities agents can invoke during a run — most
// importantly calling other IAG microservices — and a registry the agent
// runner consults to resolve tool names to executable tools.
package tools

import (
	"context"
	"encoding/json"
	"sort"

	"iag-ai-platform/backend/internal/provider"
)

// Tool is a capability an agent can call. Spec is advertised to the model;
// Execute runs it with the model-supplied JSON input and returns a string
// result that is fed back to the model.
type Tool interface {
	Spec() provider.ToolSpec
	Execute(ctx context.Context, input json.RawMessage) (string, error)
}

// Registry holds the tools available to agents.
type Registry struct {
	tools map[string]Tool
}

func NewRegistry() *Registry { return &Registry{tools: map[string]Tool{}} }

func (r *Registry) Register(t Tool) { r.tools[t.Spec().Name] = t }

func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// Names returns all registered tool names, sorted.
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.tools))
	for n := range r.tools {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Specs returns tool specs for the named tools. When names is empty, every
// registered tool is returned — so an agent with no explicit tool list gets the
// full platform toolset.
func (r *Registry) Specs(names []string) []provider.ToolSpec {
	var pick []string
	if len(names) == 0 {
		pick = r.Names()
	} else {
		pick = names
	}
	out := make([]provider.ToolSpec, 0, len(pick))
	for _, n := range pick {
		if t, ok := r.tools[n]; ok {
			out = append(out, t.Spec())
		}
	}
	return out
}

// FuncTool adapts a closure into a Tool — used for the delegate tool and other
// in-process capabilities that don't warrant their own type.
type FuncTool struct {
	spec provider.ToolSpec
	fn   func(ctx context.Context, input json.RawMessage) (string, error)
}

func NewFuncTool(name, description string, schema map[string]any, fn func(ctx context.Context, input json.RawMessage) (string, error)) *FuncTool {
	if schema == nil {
		schema = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return &FuncTool{
		spec: provider.ToolSpec{Name: name, Description: description, InputSchema: schema},
		fn:   fn,
	}
}

func (f *FuncTool) Spec() provider.ToolSpec { return f.spec }

func (f *FuncTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	return f.fn(ctx, input)
}
