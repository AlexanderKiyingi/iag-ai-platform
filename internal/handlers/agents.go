package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"iag-ai-platform/backend/internal/middleware"
	"iag-ai-platform/backend/internal/repository"
)

// ListTools returns the tools available to agents (microservice calls,
// delegation, etc.) plus the callable service catalog.
func (a *API) ListTools(c *gin.Context) {
	var services any
	if a.Services != nil {
		services = a.Services.Catalog()
	}
	c.JSON(http.StatusOK, gin.H{"tools": a.Tools.Names(), "services": services})
}

// ListAgents returns all configured agents.
func (a *API) ListAgents(c *gin.Context) {
	items, err := a.Repo.ListAgents(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list agents"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// GetAgent returns one agent by name.
func (a *API) GetAgent(c *gin.Context) {
	ag, err := a.Repo.GetAgent(c.Request.Context(), c.Param("name"))
	if errors.Is(err, repository.ErrAgentNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load agent"})
		return
	}
	c.JSON(http.StatusOK, ag)
}

type upsertAgentRequest struct {
	Name        string   `json:"name" binding:"required"`
	Description string   `json:"description"`
	System      string   `json:"system" binding:"required"`
	Model       string   `json:"model"`
	Tools       []string `json:"tools"`
}

// UpsertAgent creates or updates an agent definition.
func (a *API) UpsertAgent(c *gin.Context) {
	var req upsertAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ag, err := a.Repo.UpsertAgent(c.Request.Context(), repository.AgentInput{
		Name:        strings.TrimSpace(req.Name),
		Description: req.Description,
		System:      req.System,
		Model:       strings.TrimSpace(req.Model),
		Tools:       req.Tools,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save agent"})
		return
	}
	c.JSON(http.StatusOK, ag)
}

// DeleteAgent removes an agent definition.
func (a *API) DeleteAgent(c *gin.Context) {
	err := a.Repo.DeleteAgent(c.Request.Context(), c.Param("name"))
	if errors.Is(err, repository.ErrAgentNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not delete agent"})
		return
	}
	c.Status(http.StatusNoContent)
}

type runRequest struct {
	Task      string            `json:"task" binding:"required"`
	Variables map[string]string `json:"variables"`
}

// RunAgent runs a single named agent against a task (tool-use loop).
func (a *API) RunAgent(c *gin.Context) {
	var req runRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	a.runAgent(c, c.Param("name"), req)
}

type orchestrateRequest struct {
	Task      string            `json:"task" binding:"required"`
	Agent     string            `json:"agent"` // optional; defaults to the coordinator
	Variables map[string]string `json:"variables"`
}

// Orchestrate runs the multi-agent coordinator (or a named agent) on a task.
// The coordinator decomposes the task and delegates to specialist agents.
func (a *API) Orchestrate(c *gin.Context) {
	var req orchestrateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	name := strings.TrimSpace(req.Agent)
	if name == "" {
		name = a.Coordinator
	}
	a.runAgent(c, name, runRequest{Task: req.Task, Variables: req.Variables})
}

// runAgent is the shared run path for /agents/:name/run and /orchestrate.
func (a *API) runAgent(c *gin.Context, name string, req runRequest) {
	if a.Runner == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "agent runner not configured"})
		return
	}
	res, err := a.Runner.Run(c.Request.Context(), name, req.Task, req.Variables, middleware.Caller(c))
	if errors.Is(err, repository.ErrAgentNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found: " + name})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "agent run failed", "detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}

// GetRun returns a run and its full step trace.
func (a *API) GetRun(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid run id"})
		return
	}
	run, err := a.Repo.GetRun(c.Request.Context(), id)
	if errors.Is(err, repository.ErrRunNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "run not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load run"})
		return
	}
	c.JSON(http.StatusOK, run)
}
