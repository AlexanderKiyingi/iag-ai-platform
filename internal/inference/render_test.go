package inference

import "testing"

func TestRender(t *testing.T) {
	got := render("Hello {{name}}, invoice {{id}}", map[string]string{"name": "Acme", "id": "INV-7"})
	want := "Hello Acme, invoice INV-7"
	if got != want {
		t.Fatalf("render = %q, want %q", got, want)
	}
}

func TestRenderLeavesUnknownPlaceholders(t *testing.T) {
	got := render("Hi {{name}} {{missing}}", map[string]string{"name": "Bo"})
	want := "Hi Bo {{missing}}"
	if got != want {
		t.Fatalf("render = %q, want %q", got, want)
	}
}

func TestRenderNoVars(t *testing.T) {
	if got := render("static", nil); got != "static" {
		t.Fatalf("render = %q", got)
	}
}
