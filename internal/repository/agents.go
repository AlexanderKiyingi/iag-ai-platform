package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrAgentNotFound is returned when a named agent does not exist.
var ErrAgentNotFound = errors.New("agent not found")

// Agent is a configured AI agent: a system prompt, a model, and the tools it is
// allowed to use. An empty Tools list means "all registered tools".
type Agent struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	System      string    `json:"system"`
	Model       string    `json:"model,omitempty"`
	Tools       []string  `json:"tools"`
	Version     int       `json:"version"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// AgentInput is the create/update payload for an agent.
type AgentInput struct {
	Name        string
	Description string
	System      string
	Model       string
	Tools       []string
}

func (r *Repository) UpsertAgent(ctx context.Context, in AgentInput) (*Agent, error) {
	if in.Tools == nil {
		in.Tools = []string{}
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO ai_agents (name, description, system, model, tools)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (name) DO UPDATE SET
			description = EXCLUDED.description,
			system = EXCLUDED.system,
			model = EXCLUDED.model,
			tools = EXCLUDED.tools,
			version = ai_agents.version + 1,
			updated_at = NOW()
		RETURNING name, description, system, model, tools, version, created_at, updated_at
	`, in.Name, in.Description, in.System, in.Model, in.Tools)
	return scanAgent(row)
}

// SeedAgent inserts an agent only if it does not already exist (for boot-time
// defaults that must not clobber operator edits).
func (r *Repository) SeedAgent(ctx context.Context, in AgentInput) error {
	if in.Tools == nil {
		in.Tools = []string{}
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO ai_agents (name, description, system, model, tools)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (name) DO NOTHING
	`, in.Name, in.Description, in.System, in.Model, in.Tools)
	return err
}

func (r *Repository) GetAgent(ctx context.Context, name string) (*Agent, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT name, description, system, model, tools, version, created_at, updated_at
		FROM ai_agents WHERE name = $1
	`, name)
	a, err := scanAgent(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAgentNotFound
	}
	return a, err
}

func (r *Repository) ListAgents(ctx context.Context) ([]Agent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT name, description, system, model, tools, version, created_at, updated_at
		FROM ai_agents ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Agent{}
	for rows.Next() {
		a, err := scanAgentRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (r *Repository) DeleteAgent(ctx context.Context, name string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM ai_agents WHERE name = $1`, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAgentNotFound
	}
	return nil
}

func scanAgent(row pgx.Row) (*Agent, error) { return scanAgentRow(row) }

func scanAgentRow(row pgx.Row) (*Agent, error) {
	var a Agent
	if err := row.Scan(&a.Name, &a.Description, &a.System, &a.Model, &a.Tools,
		&a.Version, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, err
	}
	if a.Tools == nil {
		a.Tools = []string{}
	}
	return &a, nil
}
