package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alvor-technologies/iag-platform-go/authclient"
	platformotel "github.com/alvor-technologies/iag-platform-go/otel"

	"iag-ai-platform/backend/internal/agent"
	"iag-ai-platform/backend/internal/config"
	"iag-ai-platform/backend/internal/db"
	"iag-ai-platform/backend/internal/handlers"
	"iag-ai-platform/backend/internal/inference"
	"iag-ai-platform/backend/internal/middleware"
	"iag-ai-platform/backend/internal/migrate"
	"iag-ai-platform/backend/internal/provider"
	"iag-ai-platform/backend/internal/repository"
	"iag-ai-platform/backend/internal/tools"
)

const coordinatorAgent = "coordinator"

func main() {
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// OpenTelemetry → otel-collector:4317 (non-blocking dial).
	if tp, err := platformotel.Init(ctx, platformotel.Config{
		ServiceName: cfg.ServiceName,
		Environment: cfg.Environment,
	}); err != nil {
		log.Printf("otel disabled: %v", err)
	} else {
		defer func() {
			sc, c := context.WithTimeout(context.Background(), 5*time.Second)
			defer c()
			_ = tp.Shutdown(sc)
		}()
	}

	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer pool.Close()

	if cfg.AutoMigrate {
		if err := migrate.Up(ctx, pool); err != nil {
			log.Fatalf("migrate: %v", err)
		}
	}

	var verifier *authclient.Verifier
	if cfg.AuthMode == "jwt" {
		verifier = authclient.NewVerifier(authclient.Options{
			JWKSURL:  cfg.JWKSURL,
			Issuer:   cfg.JWTIssuer,
			Audience: cfg.Audience,
		})
		initCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if err := verifier.Refresh(initCtx); err != nil {
			cancel()
			log.Fatalf("jwks refresh: %v", err)
		}
		cancel()
		go jwksRefreshLoop(verifier)
	}

	platformAuth := middleware.NewPlatformAuth(middleware.PlatformAuthOptions{
		Mode:     cfg.AuthMode,
		Verifier: verifier,
	})

	go registerPermissionsLoop(ctx, cfg)

	// Inference stack: provider (Claude or stub) → service (logging) → handlers.
	prov := provider.Build(provider.Settings{
		AnthropicAPIKey:  cfg.AnthropicAPIKey,
		AnthropicBaseURL: cfg.AnthropicBaseURL,
		AnthropicVersion: cfg.AnthropicVersion,
		DefaultModel:     cfg.DefaultModel,
		MaxTokens:        cfg.MaxOutputTokens,
	})
	repo := repository.New(pool)
	inferenceSvc := inference.New(prov, repo, cfg.EmbeddingDim)
	log.Printf("ai-platform inference provider=%s default_model=%s", prov.Name(), prov.DefaultModel())

	// Tools agents can call: list_services + call_microservice (service-to-
	// service), and delegate (agent-to-agent). The service catalog comes from
	// AI_SERVICES_JSON; absent it, the call tool returns a helpful error.
	serviceCaller := tools.NewServiceCaller(tools.ServiceCallerConfig{
		Services:     parseServices(cfg.ServicesJSON),
		TokenURL:     cfg.AuthTokenURL,
		ClientID:     cfg.ServiceClientID,
		ClientSecret: cfg.ServiceClientSecret,
		AllowWrites:  cfg.AllowServiceWrites,
	})
	registry := tools.NewRegistry()
	serviceCaller.Register(registry)
	runner := agent.NewRunner(prov, registry, repo, cfg.MaxAgentSteps, cfg.MaxDelegationDepth)
	registry.Register(runner.DelegateTool()) // agent-to-agent handoff
	seedAgents(ctx, repo)
	log.Printf("ai-platform orchestrator ready: %d tools, %d services, max_steps=%d max_depth=%d writes=%v",
		len(registry.Names()), len(serviceCaller.Catalog()), cfg.MaxAgentSteps, cfg.MaxDelegationDepth, cfg.AllowServiceWrites)

	router := handlers.NewRouter(handlers.RouterDeps{
		API: &handlers.API{
			Cfg:         cfg,
			Pool:        pool,
			Inference:   inferenceSvc,
			Repo:        repo,
			Runner:      runner,
			Tools:       registry,
			Services:    serviceCaller,
			Coordinator: coordinatorAgent,
		},
		PlatformAuth: platformAuth,
		StrictRBAC:   cfg.StrictRBAC(),
	})

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second, // inference can be slow
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		log.Printf("ai-platform listening on :%s (aud=%s)", cfg.Port, cfg.Audience)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

func jwksRefreshLoop(v *authclient.Verifier) {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := v.Refresh(ctx); err != nil {
			log.Printf("jwks refresh: %v", err)
		}
		cancel()
	}
}
