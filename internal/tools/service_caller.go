package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	platformserviceauth "github.com/alvor-technologies/iag-platform-go/serviceauth"
)

// ServiceSpec describes a callable IAG microservice.
type ServiceSpec struct {
	Audience    string `json:"audience"`              // iag.<service> — token audience
	BaseURL     string `json:"baseUrl"`               // e.g. http://iag-procurement:4009
	Description string `json:"description,omitempty"` // shown to the model
}

// ServiceCaller mints per-audience service tokens and calls IAG microservices
// on the agent's behalf. The AI platform's own service principal must be
// allowed to request each target audience in iag-authentication.
type ServiceCaller struct {
	services     map[string]ServiceSpec
	tokenURL     string
	clientID     string
	clientSecret string
	allowWrites  bool
	http         *http.Client

	mu      sync.Mutex
	clients map[string]*platformserviceauth.Client
}

// ServiceCallerConfig wires the caller.
type ServiceCallerConfig struct {
	Services     map[string]ServiceSpec
	TokenURL     string
	ClientID     string
	ClientSecret string
	AllowWrites  bool // when false, only GET/HEAD are permitted (read-only)
}

func NewServiceCaller(cfg ServiceCallerConfig) *ServiceCaller {
	if cfg.Services == nil {
		cfg.Services = map[string]ServiceSpec{}
	}
	return &ServiceCaller{
		services:     cfg.Services,
		tokenURL:     cfg.TokenURL,
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		allowWrites:  cfg.AllowWrites,
		http:         &http.Client{Timeout: 30 * time.Second},
		clients:      map[string]*platformserviceauth.Client{},
	}
}

// Configured reports whether any services + credentials are wired.
func (s *ServiceCaller) Configured() bool {
	return len(s.services) > 0 && s.clientSecret != ""
}

// Catalog returns the configured services (name → spec), for discovery.
func (s *ServiceCaller) Catalog() map[string]ServiceSpec { return s.services }

func (s *ServiceCaller) tokenClient(audience string) *platformserviceauth.Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.clients[audience]; ok {
		return c
	}
	c := platformserviceauth.NewClient(platformserviceauth.Options{
		TokenURL:     s.tokenURL,
		ClientID:     s.clientID,
		ClientSecret: s.clientSecret,
		Audience:     audience,
	})
	s.clients[audience] = c
	return c
}

// ----- Tools -----

type callInput struct {
	Service string          `json:"service"`
	Method  string          `json:"method"`
	Path    string          `json:"path"`
	Query   string          `json:"query,omitempty"`
	Body    json.RawMessage `json:"body,omitempty"`
}

// CallMicroserviceTool exposes every configured backend to agents through one
// tool: the agent picks a service + REST path and the platform mints the right
// service token and forwards the call. RBAC on the target service still applies.
func (s *ServiceCaller) CallMicroserviceTool() Tool {
	return NewFuncTool(
		"call_microservice",
		"Call an IAG backend microservice's REST API. Use list_services first to see available services and their audiences. The platform handles authentication; the target service's permissions still apply.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"service": map[string]any{"type": "string", "description": "Service name from list_services (e.g. procurement, finance)."},
				"method":  map[string]any{"type": "string", "enum": []string{"GET", "POST", "PATCH", "PUT", "DELETE"}, "description": "HTTP method. Writes are disabled unless the platform allows them."},
				"path":    map[string]any{"type": "string", "description": "Request path, e.g. /api/v1/requisitions"},
				"query":   map[string]any{"type": "string", "description": "Optional raw query string without the leading '?'."},
				"body":    map[string]any{"type": "object", "description": "Optional JSON body for write methods."},
			},
			"required": []string{"service", "path"},
		},
		s.execCall,
	)
}

func (s *ServiceCaller) execCall(ctx context.Context, raw json.RawMessage) (string, error) {
	var in callInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	spec, ok := s.services[strings.TrimSpace(in.Service)]
	if !ok {
		return "", fmt.Errorf("unknown service %q; call list_services for the catalog", in.Service)
	}
	method := strings.ToUpper(strings.TrimSpace(in.Method))
	if method == "" {
		method = http.MethodGet
	}
	if !s.allowWrites && method != http.MethodGet && method != http.MethodHead {
		return "", fmt.Errorf("write methods are disabled on this AI platform (attempted %s); only GET/HEAD are permitted", method)
	}
	if !s.Configured() {
		return "", fmt.Errorf("service calling is not configured (no SERVICE_CLIENT_SECRET / service catalog)")
	}

	url := strings.TrimRight(spec.BaseURL, "/") + "/" + strings.TrimLeft(in.Path, "/")
	if q := strings.TrimSpace(in.Query); q != "" {
		url += "?" + strings.TrimLeft(q, "?")
	}
	var bodyReader io.Reader
	if len(in.Body) > 0 && method != http.MethodGet {
		bodyReader = bytes.NewReader(in.Body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return "", err
	}
	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	tok, err := s.tokenClient(spec.Audience).Token(ctx)
	if err != nil {
		return "", fmt.Errorf("mint service token for %s: %w", spec.Audience, err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)

	resp, err := s.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("call %s %s: %w", method, spec.Audience, err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024)) // cap tool output
	return fmt.Sprintf("HTTP %d\n%s", resp.StatusCode, strings.TrimSpace(string(out))), nil
}

// ListServicesTool lets the agent discover which backends it can call.
func (s *ServiceCaller) ListServicesTool() Tool {
	return NewFuncTool(
		"list_services",
		"List the IAG backend microservices available to call, with a short description of each.",
		map[string]any{"type": "object", "properties": map[string]any{}},
		func(_ context.Context, _ json.RawMessage) (string, error) {
			names := make([]string, 0, len(s.services))
			for n := range s.services {
				names = append(names, n)
			}
			sort.Strings(names)
			var sb strings.Builder
			if len(names) == 0 {
				return "No microservices are configured for AI access.", nil
			}
			for _, n := range names {
				sp := s.services[n]
				fmt.Fprintf(&sb, "- %s: %s\n", n, firstNonEmpty(sp.Description, sp.Audience))
			}
			return strings.TrimSpace(sb.String()), nil
		},
	)
}

// Register adds the service tools to a registry (no-op pieces stay usable even
// when unconfigured, returning helpful errors at call time).
func (s *ServiceCaller) Register(r *Registry) {
	r.Register(s.ListServicesTool())
	r.Register(s.CallMicroserviceTool())
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
