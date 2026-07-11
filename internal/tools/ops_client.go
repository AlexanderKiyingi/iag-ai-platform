package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	platformserviceauth "github.com/alvor-technologies/iag-platform-go/serviceauth"
)

// OpsCaller reads the iag-platform-ops monitoring API on behalf of agents.
type OpsCaller struct {
	baseURL      string
	tokenURL     string
	clientID     string
	clientSecret string
	http         *http.Client

	mu      sync.Mutex
	clients map[string]*platformserviceauth.Client
}

type OpsCallerConfig struct {
	BaseURL      string
	TokenURL     string
	ClientID     string
	ClientSecret string
}

func NewOpsCaller(cfg OpsCallerConfig) *OpsCaller {
	return &OpsCaller{
		baseURL:      strings.TrimRight(cfg.BaseURL, "/"),
		tokenURL:     cfg.TokenURL,
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		http:         &http.Client{Timeout: 30 * time.Second},
		clients:      map[string]*platformserviceauth.Client{},
	}
}

func (o *OpsCaller) Configured() bool {
	return o.baseURL != "" && o.clientSecret != ""
}

func (o *OpsCaller) tokenClient(audience string) *platformserviceauth.Client {
	o.mu.Lock()
	defer o.mu.Unlock()
	if c, ok := o.clients[audience]; ok {
		return c
	}
	c := platformserviceauth.NewClient(platformserviceauth.Options{
		TokenURL:     o.tokenURL,
		ClientID:     o.clientID,
		ClientSecret: o.clientSecret,
		Audience:     audience,
	})
	o.clients[audience] = c
	return c
}

func (o *OpsCaller) get(ctx context.Context, path string) (string, error) {
	if !o.Configured() {
		return "", fmt.Errorf("ops API not configured (set OPS_API_URL and SERVICE_CLIENT_SECRET)")
	}
	tok, err := o.tokenClient("iag.platform-ops").Token(ctx)
	if err != nil {
		return "", fmt.Errorf("mint ops token: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.baseURL+path, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := o.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 128*1024))
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("ops API HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return strings.TrimSpace(string(body)), nil
}

func (o *OpsCaller) Register(r *Registry) {
	r.Register(o.tool("get_platform_overview", "Platform health score, counts of healthy/degraded/down services, and open incidents.", map[string]any{"type": "object", "properties": map[string]any{}}, func(ctx context.Context, _ json.RawMessage) (string, error) {
		return o.get(ctx, "/api/v1/overview")
	}))
	r.Register(o.tool("list_platform_services", "List all monitored IAG microservices with latest liveness/readiness probe results.", map[string]any{"type": "object", "properties": map[string]any{}}, func(ctx context.Context, _ json.RawMessage) (string, error) {
		return o.get(ctx, "/api/v1/services")
	}))
	r.Register(o.tool("get_service_health", "Fetch recent health probe history for one service.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"serviceId": map[string]any{"type": "string", "description": "Service id from list_platform_services (e.g. finance, authentication)."},
		},
		"required": []string{"serviceId"},
	}, func(ctx context.Context, raw json.RawMessage) (string, error) {
		var in struct {
			ServiceID string `json:"serviceId"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		id := strings.TrimSpace(in.ServiceID)
		if id == "" {
			return "", fmt.Errorf("serviceId is required")
		}
		return o.get(ctx, "/api/v1/services/"+id+"/health")
	}))
	r.Register(o.tool("get_open_incidents", "List open platform incidents (service failures, degraded health).", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"status": map[string]any{"type": "string", "description": "Filter by status: open, resolved, or empty for all recent."},
		},
	}, func(ctx context.Context, raw json.RawMessage) (string, error) {
		var in struct {
			Status string `json:"status"`
		}
		_ = json.Unmarshal(raw, &in)
		path := "/api/v1/incidents"
		if s := strings.TrimSpace(in.Status); s != "" {
			path += "?status=" + s
		} else {
			path += "?status=open"
		}
		return o.get(ctx, path)
	}))
	r.Register(o.tool("get_env_audit", "Latest environment variable audit — missing required vars per service (names only, no secret values).", map[string]any{"type": "object", "properties": map[string]any{}}, func(ctx context.Context, _ json.RawMessage) (string, error) {
		return o.get(ctx, "/api/v1/env-audit")
	}))
	r.Register(o.tool("get_railway_deployments", "Latest Railway deployment status per service (requires Railway API on ops service).", map[string]any{"type": "object", "properties": map[string]any{}}, func(ctx context.Context, _ json.RawMessage) (string, error) {
		return o.get(ctx, "/api/v1/railway/deployments")
	}))
	r.Register(o.tool("get_railway_logs", "Fetch the tail of a service's latest Railway deployment logs — the primary evidence for crash loops and boot failures. Secrets are redacted server-side. Requires the Railway API on the ops service.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"serviceId": map[string]any{"type": "string", "description": "Service id from list_platform_services (e.g. finance, authentication)."},
			"limit":     map[string]any{"type": "integer", "description": "Number of recent log lines to fetch (default 200, max 1000)."},
		},
		"required": []string{"serviceId"},
	}, func(ctx context.Context, raw json.RawMessage) (string, error) {
		var in struct {
			ServiceID string `json:"serviceId"`
			Limit     int    `json:"limit"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		id := strings.TrimSpace(in.ServiceID)
		if id == "" {
			return "", fmt.Errorf("serviceId is required")
		}
		path := "/api/v1/services/" + id + "/logs"
		if in.Limit > 0 {
			path += "?limit=" + strconv.Itoa(in.Limit)
		}
		return o.get(ctx, path)
	}))
}

func (o *OpsCaller) tool(name, description string, schema map[string]any, fn func(context.Context, json.RawMessage) (string, error)) Tool {
	return NewFuncTool(name, description, schema, fn)
}

// ToolNames returns registered ops tool names sorted.
func (o *OpsCaller) ToolNames() []string {
	names := []string{
		"get_platform_overview",
		"list_platform_services",
		"get_service_health",
		"get_open_incidents",
		"get_env_audit",
		"get_railway_deployments",
		"get_railway_logs",
	}
	sort.Strings(names)
	return names
}
