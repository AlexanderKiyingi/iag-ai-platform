// Package repository persists prompt templates and inference usage logs.
package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrPromptNotFound is returned when a named prompt does not exist.
var ErrPromptNotFound = errors.New("prompt not found")

type Repository struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

// Prompt is a reusable, versioned prompt template.
type Prompt struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	System      string    `json:"system,omitempty"`
	Template    string    `json:"template"`
	Model       string    `json:"model,omitempty"`
	Version     int       `json:"version"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// PromptInput is the create/update payload for a prompt.
type PromptInput struct {
	Name        string
	Description string
	System      string
	Template    string
	Model       string
}

// UpsertPrompt creates a prompt or updates it in place, bumping the version on
// each update so changes are auditable.
func (r *Repository) UpsertPrompt(ctx context.Context, in PromptInput) (*Prompt, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO ai_prompts (name, description, system, template, model)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (name) DO UPDATE SET
			description = EXCLUDED.description,
			system = EXCLUDED.system,
			template = EXCLUDED.template,
			model = EXCLUDED.model,
			version = ai_prompts.version + 1,
			updated_at = NOW()
		RETURNING name, description, system, template, model, version, created_at, updated_at
	`, in.Name, in.Description, in.System, in.Template, in.Model)
	return scanPrompt(row)
}

// GetPrompt returns a prompt by name.
func (r *Repository) GetPrompt(ctx context.Context, name string) (*Prompt, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT name, description, system, template, model, version, created_at, updated_at
		FROM ai_prompts WHERE name = $1
	`, name)
	p, err := scanPrompt(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPromptNotFound
	}
	return p, err
}

// ListPrompts returns all prompts, alphabetically.
func (r *Repository) ListPrompts(ctx context.Context) ([]Prompt, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT name, description, system, template, model, version, created_at, updated_at
		FROM ai_prompts ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Prompt{}
	for rows.Next() {
		p, err := scanPromptRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// DeletePrompt removes a prompt. Returns ErrPromptNotFound if absent.
func (r *Repository) DeletePrompt(ctx context.Context, name string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM ai_prompts WHERE name = $1`, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrPromptNotFound
	}
	return nil
}

// RequestLog is one inference call recorded for usage/cost attribution.
type RequestLog struct {
	Caller       string
	Kind         string // completion | embedding | prompt
	PromptName   string
	Provider     string
	Model        string
	InputTokens  int
	OutputTokens int
	LatencyMS    int
	Status       string
	Error        string
}

// LogRequest records an inference call. Best-effort: a logging failure must not
// fail the caller's request, so the caller logs-and-continues on error.
func (r *Repository) LogRequest(ctx context.Context, l RequestLog) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO ai_requests
			(caller, kind, prompt_name, provider, model, input_tokens, output_tokens, latency_ms, status, error)
		VALUES ($1, $2, NULLIF($3, ''), $4, $5, $6, $7, $8, $9, NULLIF($10, ''))
	`, l.Caller, l.Kind, l.PromptName, l.Provider, l.Model,
		l.InputTokens, l.OutputTokens, l.LatencyMS, l.Status, l.Error)
	return err
}

// UsageRow aggregates usage for one (caller, model) pair.
type UsageRow struct {
	Caller       string `json:"caller"`
	Model        string `json:"model"`
	Requests     int    `json:"requests"`
	InputTokens  int    `json:"inputTokens"`
	OutputTokens int    `json:"outputTokens"`
	Errors       int    `json:"errors"`
}

// UsageSummary aggregates the last `sinceHours` of usage by caller and model.
func (r *Repository) UsageSummary(ctx context.Context, sinceHours int) ([]UsageRow, error) {
	if sinceHours <= 0 {
		sinceHours = 24
	}
	rows, err := r.pool.Query(ctx, `
		SELECT caller, model,
		       COUNT(*)::int,
		       COALESCE(SUM(input_tokens), 0)::int,
		       COALESCE(SUM(output_tokens), 0)::int,
		       COALESCE(SUM(CASE WHEN status <> 'ok' THEN 1 ELSE 0 END), 0)::int
		FROM ai_requests
		WHERE created_at >= NOW() - ($1 || ' hours')::interval
		GROUP BY caller, model
		ORDER BY 3 DESC
	`, sinceHours)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UsageRow{}
	for rows.Next() {
		var u UsageRow
		if err := rows.Scan(&u.Caller, &u.Model, &u.Requests, &u.InputTokens, &u.OutputTokens, &u.Errors); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func scanPrompt(row pgx.Row) (*Prompt, error) { return scanPromptRow(row) }

func scanPromptRow(row pgx.Row) (*Prompt, error) {
	var p Prompt
	if err := row.Scan(&p.Name, &p.Description, &p.System, &p.Template, &p.Model,
		&p.Version, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	return &p, nil
}
