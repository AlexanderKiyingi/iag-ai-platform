package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"

	"iag-ai-platform/backend/internal/agent"
	"iag-ai-platform/backend/internal/config"
	"iag-ai-platform/backend/internal/db"
	"iag-ai-platform/backend/internal/inference"
	"iag-ai-platform/backend/internal/middleware"
	"iag-ai-platform/backend/internal/repository"
	"iag-ai-platform/backend/internal/tools"
)

// API holds handler dependencies.
type API struct {
	Cfg         config.Config
	Pool        *pgxpool.Pool
	Inference   *inference.Service
	Repo        *repository.Repository
	Runner      *agent.Runner
	Tools       *tools.Registry
	Services    *tools.ServiceCaller
	Coordinator string // default agent for /orchestrate
}

// RouterDeps wires the router.
type RouterDeps struct {
	API          *API
	PlatformAuth *middleware.PlatformAuth
	StrictRBAC   bool
}

func NewRouter(deps RouterDeps) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(otelgin.Middleware(deps.API.Cfg.ServiceName))
	r.Use(gin.Recovery())
	if deps.PlatformAuth != nil {
		r.Use(deps.PlatformAuth.AttachPrincipal())
	}

	a := deps.API
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": a.Cfg.ServiceName})
	})
	r.GET("/ready", func(c *gin.Context) {
		if err := db.Ping(c.Request.Context(), a.Pool); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "degraded", "database": false})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready", "database": true, "provider": a.Inference.ProviderName()})
	})

	v1 := r.Group("/api/v1")
	if deps.PlatformAuth != nil {
		v1.Use(deps.PlatformAuth.RequireAuth())
	}
	if deps.StrictRBAC {
		v1.Use(middleware.StrictRBAC())
	}
	{
		v1.GET("/overview", middleware.RequirePermission("ai.use_inference"), a.Overview)

		// Inference — the primitives every microservice calls.
		v1.POST("/completions", middleware.RequirePermission("ai.use_inference"), a.Completions)
		v1.POST("/embeddings", middleware.RequirePermission("ai.use_inference"), a.Embeddings)

		// Shared prompt registry.
		v1.GET("/prompts", middleware.RequirePermission("ai.manage_prompts"), a.ListPrompts)
		v1.POST("/prompts", middleware.RequirePermission("ai.manage_prompts"), a.UpsertPrompt)
		v1.GET("/prompts/:name", middleware.RequirePermission("ai.manage_prompts"), a.GetPrompt)
		v1.DELETE("/prompts/:name", middleware.RequirePermission("ai.manage_prompts"), a.DeletePrompt)
		v1.POST("/prompts/:name/run", middleware.RequirePermission("ai.use_inference"), a.RunPrompt)

		// Multi-agent orchestration.
		v1.GET("/tools", middleware.RequirePermission("ai.run_agents"), a.ListTools)
		v1.GET("/agents", middleware.RequirePermission("ai.manage_agents"), a.ListAgents)
		v1.POST("/agents", middleware.RequirePermission("ai.manage_agents"), a.UpsertAgent)
		v1.GET("/agents/:name", middleware.RequirePermission("ai.manage_agents"), a.GetAgent)
		v1.DELETE("/agents/:name", middleware.RequirePermission("ai.manage_agents"), a.DeleteAgent)
		v1.POST("/agents/:name/run", middleware.RequirePermission("ai.run_agents"), a.RunAgent)
		v1.POST("/orchestrate", middleware.RequirePermission("ai.run_agents"), a.Orchestrate)
		v1.GET("/runs/:id", middleware.RequirePermission("ai.run_agents"), a.GetRun)

		// Usage / cost attribution.
		v1.GET("/usage", middleware.RequirePermission("ai.view_usage"), a.Usage)
	}
	return r
}
