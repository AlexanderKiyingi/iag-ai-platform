package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// Overview reports the service status and active inference configuration.
func (a *API) Overview(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"service":      a.Cfg.ServiceName,
		"environment":  a.Cfg.Environment,
		"provider":     a.Inference.ProviderName(),
		"defaultModel": a.Inference.DefaultModel(),
		"capabilities": []string{"completions", "embeddings", "prompts", "agents", "orchestrate", "usage"},
		"tools":        a.Tools.Names(),
	})
}

// Usage returns aggregated AI usage by caller and model. Query: ?hours=N (default 24).
func (a *API) Usage(c *gin.Context) {
	hours := 24
	if raw := c.Query("hours"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			hours = n
		}
	}
	rows, err := a.Repo.UsageSummary(c.Request.Context(), hours)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load usage"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"windowHours": hours, "items": rows})
}
