package agent

import "context"

type ctxKey int

const (
	callerKey ctxKey = iota
	depthKey
)

// withCaller stamps the principal that initiated the run so tools (and
// delegated sub-runs) can attribute usage consistently.
func withCaller(ctx context.Context, caller string) context.Context {
	return context.WithValue(ctx, callerKey, caller)
}

func callerFrom(ctx context.Context) string {
	if v, ok := ctx.Value(callerKey).(string); ok {
		return v
	}
	return "anonymous"
}

// deeper increments the delegation depth for a sub-run.
func deeper(ctx context.Context) context.Context {
	return context.WithValue(ctx, depthKey, depth(ctx)+1)
}

func depth(ctx context.Context) int {
	if v, ok := ctx.Value(depthKey).(int); ok {
		return v
	}
	return 0
}
