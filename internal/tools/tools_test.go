package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRegistryAndFuncTool(t *testing.T) {
	reg := NewRegistry()
	called := false
	reg.Register(NewFuncTool("echo", "echoes input", nil, func(_ context.Context, in json.RawMessage) (string, error) {
		called = true
		return string(in), nil
	}))

	if got := reg.Names(); len(got) != 1 || got[0] != "echo" {
		t.Fatalf("Names = %v", got)
	}
	tool, ok := reg.Get("echo")
	if !ok {
		t.Fatal("echo tool not found")
	}
	out, err := tool.Execute(context.Background(), json.RawMessage(`{"x":1}`))
	if err != nil || out != `{"x":1}` || !called {
		t.Fatalf("execute = %q, err=%v, called=%v", out, err, called)
	}
}

func TestSpecsFilterAndAll(t *testing.T) {
	reg := NewRegistry()
	reg.Register(NewFuncTool("a", "", nil, nil))
	reg.Register(NewFuncTool("b", "", nil, nil))

	if all := reg.Specs(nil); len(all) != 2 {
		t.Fatalf("Specs(nil) returned %d, want all 2", len(all))
	}
	pick := reg.Specs([]string{"b", "missing"})
	if len(pick) != 1 || pick[0].Name != "b" {
		t.Fatalf("Specs filter = %+v", pick)
	}
}

func TestCallMicroserviceUnconfigured(t *testing.T) {
	sc := NewServiceCaller(ServiceCallerConfig{}) // no services, no creds
	out, err := sc.CallMicroserviceTool().Execute(context.Background(), json.RawMessage(`{"service":"finance","path":"/x"}`))
	if err == nil {
		t.Fatalf("expected error for unknown service, got %q", out)
	}
}

func TestCallMicroserviceWriteBlocked(t *testing.T) {
	sc := NewServiceCaller(ServiceCallerConfig{
		Services:     map[string]ServiceSpec{"finance": {Audience: "iag.finance", BaseURL: "http://x"}},
		ClientSecret: "secret",
		AllowWrites:  false,
	})
	_, err := sc.CallMicroserviceTool().Execute(context.Background(), json.RawMessage(`{"service":"finance","method":"POST","path":"/x"}`))
	if err == nil {
		t.Fatal("expected write to be blocked when AllowWrites=false")
	}
}
