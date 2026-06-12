-- iag-ai-platform initial schema.
--
-- The AI platform is a shared inference + prompt gateway other IAG services
-- call. It persists:
--   * ai_prompts   — reusable, versioned prompt templates services run by name
--   * ai_requests  — one row per inference call for usage/cost attribution
--   * ai_service_meta — boot marker
-- Statements are separated by blank lines because the migrator splits on ";\n\n".

CREATE TABLE IF NOT EXISTS ai_service_meta (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO ai_service_meta (key, value)
VALUES ('schema_initialized', NOW()::text)
ON CONFLICT (key) DO NOTHING;

-- Reusable prompt templates. `template` uses {{var}} placeholders substituted
-- at run time. `version` bumps on every update so changes are auditable.
CREATE TABLE IF NOT EXISTS ai_prompts (
    name        TEXT PRIMARY KEY,
    description TEXT NOT NULL DEFAULT '',
    system      TEXT NOT NULL DEFAULT '',
    template    TEXT NOT NULL,
    model       TEXT NOT NULL DEFAULT '',
    version     INT  NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- One row per inference call, for usage/cost attribution per calling service.
CREATE TABLE IF NOT EXISTS ai_requests (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    caller        TEXT NOT NULL DEFAULT '',
    kind          TEXT NOT NULL,             -- completion | embedding | prompt
    prompt_name   TEXT,
    provider      TEXT NOT NULL DEFAULT '',
    model         TEXT NOT NULL DEFAULT '',
    input_tokens  INT  NOT NULL DEFAULT 0,
    output_tokens INT  NOT NULL DEFAULT 0,
    latency_ms    INT  NOT NULL DEFAULT 0,
    status        TEXT NOT NULL DEFAULT 'ok',
    error         TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ai_requests_caller_time ON ai_requests (caller, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_ai_requests_time ON ai_requests (created_at DESC);
