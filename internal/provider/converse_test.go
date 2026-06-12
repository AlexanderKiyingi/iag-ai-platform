package provider

import (
	"context"
	"testing"
)

func TestStubConverseTerminates(t *testing.T) {
	s := NewStub("m")
	res, err := s.Converse(context.Background(), ConverseRequest{
		Turns: []Turn{{Role: RoleUser, Text: "do something"}},
		Tools: []ToolSpec{{Name: "t", Description: "d"}},
	})
	if err != nil {
		t.Fatalf("converse: %v", err)
	}
	if res.StopReason != "end_turn" {
		t.Fatalf("stop reason = %q, want end_turn (stub must not loop)", res.StopReason)
	}
	if len(res.ToolCalls) != 0 {
		t.Fatalf("stub should not issue tool calls, got %d", len(res.ToolCalls))
	}
	if res.Text == "" {
		t.Fatal("expected non-empty text")
	}
}
