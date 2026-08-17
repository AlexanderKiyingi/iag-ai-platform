package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/alvor-technologies/iag-platform-go/corsenv"
	"github.com/joho/godotenv"
)

// Config holds the platform-standard service configuration plus the AI
// provider settings. When ANTHROPIC_API_KEY is unset the service runs against a
// deterministic stub provider, so local dev needs no external API key.
type Config struct {
	Environment string
	ServiceName string
	Port        string
	LogLevel    string

	DatabaseURL string
	AutoMigrate bool

	AuthMode            string
	JWTIssuer           string
	JWKSURL             string
	Audience            string
	ServiceClientID     string
	ServiceClientSecret string
	AuthTokenURL        string
	CORSOrigins         []string
	// TrustedProxies pins gin's trusted proxies (gateway/edge CIDR) so the rate
	// limiter keys on the real, non-spoofable client IP. Empty = gin default.
	TrustedProxies []string
	// RateLimitPerMin is the per-caller budget on the /api/v1 API. Inference and
	// orchestration are expensive, so this defends compute/cost, not just abuse.
	RateLimitPerMin int

	// AI provider
	AnthropicAPIKey  string
	AnthropicBaseURL string
	AnthropicVersion string
	DefaultModel     string
	MaxOutputTokens  int
	EmbeddingDim     int

	// Orchestration
	ServicesJSON       string // AI_SERVICES_JSON
	OpsAPIURL          string // OPS_API_URL — iag-platform-ops base URL for ops agent tools
	MaxAgentSteps      int
	MaxDelegationDepth int
	AllowServiceWrites bool
}

func Load() (Config, error) {
	_ = godotenv.Load()

	env := strings.ToLower(strings.TrimSpace(getenv("ENVIRONMENT", "development")))
	authMode := strings.ToLower(strings.TrimSpace(getenv("AUTH_MODE", "jwt")))
	if authMode != "jwt" {
		return Config{}, fmt.Errorf("AUTH_MODE must be jwt (got %q)", authMode)
	}

	c := Config{
		Environment:         env,
		ServiceName:         getenv("SERVICE_NAME", "ai-platform"),
		Port:                getenv("PORT", "3007"),
		LogLevel:            getenv("LOG_LEVEL", "info"),
		DatabaseURL:         strings.TrimSpace(os.Getenv("DATABASE_URL")),
		AutoMigrate:         getenv("AUTO_MIGRATE", "true") != "false",
		AuthMode:            authMode,
		JWTIssuer:           getenv("JWT_ISSUER", "http://localhost:3001"),
		JWKSURL:             getenv("JWKS_URL", "http://localhost:3001/.well-known/jwks.json"),
		Audience:            getenv("AUDIENCE", "iag.ai-platform"),
		ServiceClientID:     getenv("SERVICE_CLIENT_ID", "iag-ai-platform"),
		ServiceClientSecret: os.Getenv("SERVICE_CLIENT_SECRET"),
		CORSOrigins:         splitCSV(corsenv.Allowlist("http://localhost:3000,http://localhost:8080")),
		TrustedProxies:      splitCSV(os.Getenv("TRUSTED_PROXIES")),
		RateLimitPerMin:     getInt("RATE_LIMIT_PER_MIN", 120),

		AnthropicAPIKey:  strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")),
		AnthropicBaseURL: getenv("ANTHROPIC_BASE_URL", "https://api.anthropic.com"),
		AnthropicVersion: getenv("ANTHROPIC_VERSION", "2023-06-01"),
		DefaultModel:     getenv("AI_DEFAULT_MODEL", "claude-sonnet-4-6"),
		MaxOutputTokens:  getInt("AI_MAX_OUTPUT_TOKENS", 1024),
		EmbeddingDim:     getInt("AI_EMBEDDING_DIM", 256),

		ServicesJSON:       strings.TrimSpace(os.Getenv("AI_SERVICES_JSON")),
		OpsAPIURL:          strings.TrimRight(strings.TrimSpace(os.Getenv("OPS_API_URL")), "/"),
		MaxAgentSteps:      getInt("AI_MAX_AGENT_STEPS", 8),
		MaxDelegationDepth: getInt("AI_MAX_DELEGATION_DEPTH", 3),
		AllowServiceWrites: strings.EqualFold(getenv("AI_ALLOW_SERVICE_WRITES", "false"), "true"),
	}

	if c.DatabaseURL == "" {
		return c, fmt.Errorf("DATABASE_URL is required")
	}
	if c.AuthTokenURL == "" {
		c.AuthTokenURL = strings.TrimRight(c.JWTIssuer, "/") + "/oauth/token"
	}
	if c.IsProduction() {
		if c.ServiceClientSecret == "" {
			return c, fmt.Errorf("SERVICE_CLIENT_SECRET is required in production")
		}
		if len(c.ServiceClientSecret) < 16 {
			return c, fmt.Errorf("SERVICE_CLIENT_SECRET must be at least 16 characters in production")
		}
		if c.AutoMigrate {
			return c, fmt.Errorf("AUTO_MIGRATE must be false in production (run migrations out of band)")
		}
		if c.AnthropicAPIKey == "" {
			return c, fmt.Errorf("ANTHROPIC_API_KEY is required in production (stub provider is dev-only)")
		}
	}
	return c, nil
}

// ProviderName reports which inference provider will be used given the config.
func (c Config) ProviderName() string {
	if c.AnthropicAPIKey != "" {
		return "anthropic"
	}
	return "stub"
}

func (c Config) IsProduction() bool {
	return c.Environment == "production" || c.Environment == "prod"
}

// StrictRBAC fails permission checks closed in production; open in dev/test.
// StrictRBAC denies access when a verified token carries no permissions
// (fail-closed).
func (c Config) StrictRBAC() bool { return c.HardenedRuntime() }

// HardenedRuntime reports whether production safeguards apply.
//
// It deliberately does not just return IsProduction(). That required
// ENVIRONMENT=production, which the Railway runbooks never told anyone to set,
// so a hosted instance fell back to the "development" default and ran
// fail-OPEN: the permission middleware grants EVERY permission to a token
// carrying an empty permissions array. An unset ENVIRONMENT on a deployed
// instance now hardens instead; only an explicit dev-like value opts out.
//
// This cannot prevent boot — the worst case is a 403 for a caller that should
// never have had access. Boot-time validation stays keyed on ENVIRONMENT alone.
//
// Mirrors iag-fleet's config.HardenedRuntime; the intent is one shared
// implementation in shared/platform-go once every service is on it.
func (c Config) HardenedRuntime() bool {
	// An explicit production value always hardens, including on a Config built
	// by hand in a test rather than through Load.
	if c.IsProduction() {
		return true
	}
	if environmentExplicitlySet() {
		return !c.isDevLike()
	}
	return deployedRuntime()
}

// isDevLike reports an environment where fail-open behaviour is a deliberate
// local convenience rather than an accident.
func (c Config) isDevLike() bool {
	switch c.Environment {
	case "development", "dev", "local", "test":
		return true
	}
	return false
}

// environmentExplicitlySet distinguishes a deliberately configured environment
// from the "development" value Load falls back to when nothing is set. Read
// from the process rather than captured on Config: StrictRBAC is resolved once
// during startup wiring, and the environment does not change under us.
func environmentExplicitlySet() bool {
	return strings.TrimSpace(os.Getenv("ENVIRONMENT")) != "" ||
		strings.TrimSpace(os.Getenv("APP_ENV")) != ""
}

// deployedRuntime distinguishes a hosted instance from a laptop: Railway's
// injected variables, or gin in release mode, which the Dockerfiles set.
func deployedRuntime() bool {
	if strings.TrimSpace(os.Getenv("RAILWAY_ENVIRONMENT")) != "" ||
		strings.TrimSpace(os.Getenv("RAILWAY_PROJECT_ID")) != "" {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(os.Getenv("GIN_MODE")), "release")
}

func getenv(k, d string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return d
}

func getInt(k string, d int) int {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return d
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
