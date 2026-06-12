package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrRunNotFound is returned when a run id is unknown.
var ErrRunNotFound = errors.New("run not found")

// Run is one agent/orchestrator execution and its step trace.
type Run struct {
	ID        uuid.UUID `json:"id"`
	Agent     string    `json:"agent"`
	Caller    string    `json:"caller"`
	Task      string    `json:"task"`
	Status    string    `json:"status"` // running | ok | error | max_steps
	Output    string    `json:"output"`
	Steps     int       `json:"steps"`
	CreatedAt time.Time `json:"createdAt"`
	Trace     []RunStep `json:"trace,omitempty"`
}

// RunStep is one step in a run's trace (an assistant turn or a tool result).
type RunStep struct {
	Step      int       `json:"step"`
	Kind      string    `json:"kind"` // assistant | tool
	Name      string    `json:"name,omitempty"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"createdAt"`
}

// CreateRun opens a run in the 'running' state and returns its id.
func (r *Repository) CreateRun(ctx context.Context, agent, caller, task string) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(ctx, `
		INSERT INTO ai_runs (agent, caller, task, status) VALUES ($1, $2, $3, 'running')
		RETURNING id
	`, agent, caller, task).Scan(&id)
	return id, err
}

// AppendStep records one step of a run's trace.
func (r *Repository) AppendStep(ctx context.Context, runID uuid.UUID, step int, kind, name, content string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO ai_run_steps (run_id, step, kind, name, content)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5)
	`, runID, step, kind, name, content)
	return err
}

// FinishRun records the terminal status, output, and step count.
func (r *Repository) FinishRun(ctx context.Context, runID uuid.UUID, status, output string, steps int) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE ai_runs SET status = $2, output = $3, steps = $4 WHERE id = $1
	`, runID, status, output, steps)
	return err
}

// GetRun returns a run and its full step trace.
func (r *Repository) GetRun(ctx context.Context, id uuid.UUID) (*Run, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, agent, caller, task, status, COALESCE(output, ''), steps, created_at
		FROM ai_runs WHERE id = $1
	`, id)
	var run Run
	if err := row.Scan(&run.ID, &run.Agent, &run.Caller, &run.Task, &run.Status, &run.Output, &run.Steps, &run.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRunNotFound
		}
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `
		SELECT step, kind, COALESCE(name, ''), content, created_at
		FROM ai_run_steps WHERE run_id = $1 ORDER BY step, created_at
	`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var s RunStep
		if err := rows.Scan(&s.Step, &s.Kind, &s.Name, &s.Content, &s.CreatedAt); err != nil {
			return nil, err
		}
		run.Trace = append(run.Trace, s)
	}
	return &run, rows.Err()
}
