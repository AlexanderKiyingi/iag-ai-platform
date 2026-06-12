package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"iag-ai-platform/backend/internal/inference"
	"iag-ai-platform/backend/internal/middleware"
	"iag-ai-platform/backend/internal/provider"
)

type completionsRequest struct {
	Model       string             `json:"model"`
	System      string             `json:"system"`
	Prompt      string             `json:"prompt"`   // shorthand for a single user message
	Messages    []provider.Message `json:"messages"` // full chat; takes precedence over prompt
	MaxTokens   int                `json:"maxTokens"`
	Temperature float64            `json:"temperature"`
}

// Completions runs a chat/text completion via the configured provider.
func (a *API) Completions(c *gin.Context) {
	var req completionsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	msgs := req.Messages
	if len(msgs) == 0 {
		if strings.TrimSpace(req.Prompt) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "provide either 'messages' or 'prompt'"})
			return
		}
		msgs = []provider.Message{{Role: provider.RoleUser, Content: req.Prompt}}
	}
	res, err := a.Inference.Complete(c.Request.Context(), inference.CompletionInput{
		Model:       req.Model,
		System:      req.System,
		Messages:    msgs,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	}, middleware.Caller(c))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "inference failed", "detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}

type embeddingsRequest struct {
	Input  string   `json:"input"`  // single text
	Inputs []string `json:"inputs"` // batch; takes precedence
}

// Embeddings returns deterministic vector embeddings for the input text(s).
func (a *API) Embeddings(c *gin.Context) {
	var req embeddingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	inputs := req.Inputs
	if len(inputs) == 0 {
		if strings.TrimSpace(req.Input) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "provide either 'inputs' or 'input'"})
			return
		}
		inputs = []string{req.Input}
	}
	res, err := a.Inference.Embed(c.Request.Context(), inputs, middleware.Caller(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "embedding failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"model":      res.Model,
		"dimensions": res.Dim,
		"embeddings": res.Vectors,
	})
}
