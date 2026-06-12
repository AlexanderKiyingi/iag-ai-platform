package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"iag-ai-platform/backend/internal/middleware"
	"iag-ai-platform/backend/internal/repository"
)

// ListPrompts returns all registered prompt templates.
func (a *API) ListPrompts(c *gin.Context) {
	items, err := a.Repo.ListPrompts(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list prompts"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// GetPrompt returns a single prompt by name.
func (a *API) GetPrompt(c *gin.Context) {
	p, err := a.Repo.GetPrompt(c.Request.Context(), c.Param("name"))
	if errors.Is(err, repository.ErrPromptNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "prompt not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load prompt"})
		return
	}
	c.JSON(http.StatusOK, p)
}

type upsertPromptRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
	System      string `json:"system"`
	Template    string `json:"template" binding:"required"`
	Model       string `json:"model"`
}

// UpsertPrompt creates or updates a prompt template (version bumps on update).
func (a *API) UpsertPrompt(c *gin.Context) {
	var req upsertPromptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	p, err := a.Repo.UpsertPrompt(c.Request.Context(), repository.PromptInput{
		Name:        strings.TrimSpace(req.Name),
		Description: req.Description,
		System:      req.System,
		Template:    req.Template,
		Model:       strings.TrimSpace(req.Model),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save prompt"})
		return
	}
	c.JSON(http.StatusOK, p)
}

// DeletePrompt removes a prompt template.
func (a *API) DeletePrompt(c *gin.Context) {
	err := a.Repo.DeletePrompt(c.Request.Context(), c.Param("name"))
	if errors.Is(err, repository.ErrPromptNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "prompt not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not delete prompt"})
		return
	}
	c.Status(http.StatusNoContent)
}

type runPromptRequest struct {
	Variables map[string]string `json:"variables"`
	Model     string            `json:"model"` // optional override
}

// RunPrompt renders a saved prompt with variables and runs it through the provider.
func (a *API) RunPrompt(c *gin.Context) {
	var req runPromptRequest
	if err := c.ShouldBindJSON(&req); err != nil && err.Error() != "EOF" {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	res, prompt, err := a.Inference.RunPrompt(c.Request.Context(), c.Param("name"), req.Variables, strings.TrimSpace(req.Model), middleware.Caller(c))
	if errors.Is(err, repository.ErrPromptNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "prompt not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "inference failed", "detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"prompt":     prompt.Name,
		"version":    prompt.Version,
		"completion": res,
	})
}
