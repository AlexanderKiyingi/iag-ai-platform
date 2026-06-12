package provider

import (
	"context"
	"math"
	"testing"
)

func TestStubCompleteDeterministic(t *testing.T) {
	s := NewStub("test-model")
	req := CompletionRequest{Messages: []Message{{Role: RoleUser, Content: "hello world"}}}
	a, err := s.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	b, _ := s.Complete(context.Background(), req)
	if a.Text != b.Text {
		t.Fatalf("stub not deterministic: %q vs %q", a.Text, b.Text)
	}
	if a.Provider != "stub" || a.Model != "test-model" {
		t.Fatalf("unexpected provider/model: %+v", a)
	}
	if a.Usage.OutputTokens == 0 {
		t.Fatalf("expected non-zero output tokens")
	}
}

func TestEmbedDeterministicNormalizedDim(t *testing.T) {
	const dim = 128
	v1 := Embed([]string{"the quick brown fox"}, dim)
	v2 := Embed([]string{"the quick brown fox"}, dim)
	if len(v1) != 1 || len(v1[0]) != dim {
		t.Fatalf("expected 1x%d, got %dx%d", dim, len(v1), len(v1[0]))
	}
	for i := range v1[0] {
		if v1[0][i] != v2[0][i] {
			t.Fatalf("embedding not deterministic at %d", i)
		}
	}
	var sum float64
	for _, x := range v1[0] {
		sum += x * x
	}
	if math.Abs(math.Sqrt(sum)-1.0) > 1e-9 {
		t.Fatalf("embedding not L2-normalized: norm=%v", math.Sqrt(sum))
	}
}

func TestEmbedDistinguishesText(t *testing.T) {
	v := Embed([]string{"finance invoice", "fleet vehicle"}, 64)
	same := true
	for i := range v[0] {
		if v[0][i] != v[1][i] {
			same = false
			break
		}
	}
	if same {
		t.Fatalf("distinct inputs produced identical embeddings")
	}
}
