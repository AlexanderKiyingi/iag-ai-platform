-- Multi-agent orchestration: configured agents and per-run execution traces.
-- Statements are separated by blank lines because the migrator splits on ";\n\n".

-- An agent is a system prompt + model + the set of tools it may call. An empty
-- tools array means "all registered tools". Default agents are seeded at boot.
CREATE TABLE IF NOT EXISTS ai_agents (
    name        TEXT PRIMARY KEY,
    description TEXT NOT NULL DEFAULT '',
    system      TEXT NOT NULL DEFAULT '',
    model       TEXT NOT NULL DEFAULT '',
    tools       TEXT[] NOT NULL DEFAULT '{}',
    version     INT  NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- One row per agent/orchestrator execution.
CREATE TABLE IF NOT EXISTS ai_runs (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent      TEXT NOT NULL,
    caller     TEXT NOT NULL DEFAULT '',
    task       TEXT NOT NULL DEFAULT '',
    status     TEXT NOT NULL DEFAULT 'running',
    output     TEXT,
    steps      INT  NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ai_runs_time ON ai_runs (created_at DESC);

-- The ordered step trace for a run (assistant turns and tool results).
CREATE TABLE IF NOT EXISTS ai_run_steps (
    id         BIGSERIAL PRIMARY KEY,
    run_id     UUID NOT NULL REFERENCES ai_runs (id) ON DELETE CASCADE,
    step       INT  NOT NULL,
    kind       TEXT NOT NULL,             -- assistant | tool
    name       TEXT,                       -- tool name for tool steps
    content    TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ai_run_steps_run ON ai_run_steps (run_id, step);
