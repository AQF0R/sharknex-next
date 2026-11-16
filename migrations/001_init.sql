CREATE TABLE runs (
    id UUID PRIMARY KEY,
    name VARCHAR(255) NOT NULL,

    source VARCHAR(50) NOT NULL DEFAULT 'local',
    mode VARCHAR(50) NOT NULL DEFAULT 'normal',

    status VARCHAR(32) NOT NULL DEFAULT 'created',

    scope JSONB NOT NULL DEFAULT '{}',
    config JSONB NOT NULL DEFAULT '{}',

    max_workers INT NOT NULL DEFAULT 5,
    max_tasks INT NOT NULL DEFAULT 100,

    deadline_at TIMESTAMPTZ,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_runs_status
ON runs(status);

CREATE INDEX idx_runs_source
ON runs(source);

CREATE TABLE goals (
    id UUID PRIMARY KEY,
    run_id UUID NOT NULL REFERENCES runs(id),

    parent_id UUID REFERENCES goals(id),

    name VARCHAR(255) NOT NULL,
    description TEXT,

    status VARCHAR(32) NOT NULL DEFAULT 'pending',

    priority INT NOT NULL DEFAULT 50,

    success_criteria JSONB NOT NULL DEFAULT '{}',

    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_goals_run_status
ON goals(run_id, status);

CREATE TABLE tasks (
    id UUID PRIMARY KEY,
    run_id UUID NOT NULL REFERENCES runs(id),

    goal_id UUID REFERENCES goals(id),
    parent_id UUID REFERENCES tasks(id),

    task_type VARCHAR(100) NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT,

    status VARCHAR(32) NOT NULL DEFAULT 'pending',

    priority INT NOT NULL DEFAULT 50,

    required_capabilities JSONB NOT NULL DEFAULT '[]',

    input JSONB NOT NULL DEFAULT '{}',
    result JSONB NOT NULL DEFAULT '{}',

    created_by VARCHAR(50) NOT NULL DEFAULT 'planner',

    retry_count INT NOT NULL DEFAULT 0,
    max_retries INT NOT NULL DEFAULT 2,

    available_at TIMESTAMPTZ DEFAULT NOW(),
    deadline_at TIMESTAMPTZ,

    assigned_worker_id UUID,

    idempotency_key VARCHAR(255),

    created_at TIMESTAMPTZ DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ DEFAULT NOW(),

    UNIQUE(run_id, idempotency_key)
);

CREATE INDEX idx_tasks_ready
ON tasks(run_id, priority DESC, created_at)
WHERE status = 'ready';

CREATE INDEX idx_tasks_worker
ON tasks(assigned_worker_id, status);

CREATE INDEX idx_tasks_parent
ON tasks(parent_id);

CREATE INDEX idx_tasks_type
ON tasks(run_id, task_type);

CREATE TABLE task_dependencies (
    id UUID PRIMARY KEY,

    task_id UUID NOT NULL REFERENCES tasks(id),
    depends_on UUID NOT NULL REFERENCES tasks(id),

    dependency_type VARCHAR(32) NOT NULL DEFAULT 'success',

    created_at TIMESTAMPTZ DEFAULT NOW(),

    UNIQUE(task_id, depends_on),

    CHECK(task_id <> depends_on)
);

CREATE INDEX idx_task_dependencies_task
ON task_dependencies(task_id);

CREATE INDEX idx_task_dependencies_parent
ON task_dependencies(depends_on);

CREATE TABLE workers (
    id UUID PRIMARY KEY,

    name VARCHAR(100) NOT NULL,

    runtime_id VARCHAR(100),
    status VARCHAR(32) NOT NULL DEFAULT 'registered',

    max_concurrency INT NOT NULL DEFAULT 1,
    active_tasks INT NOT NULL DEFAULT 0,

    runtime_metadata JSONB NOT NULL DEFAULT '{}',

    last_heartbeat_at TIMESTAMPTZ,
    registered_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_workers_status
ON workers(status);

CREATE TABLE worker_capabilities (
    worker_id UUID NOT NULL REFERENCES workers(id),

    capability VARCHAR(100) NOT NULL,

    enabled BOOLEAN NOT NULL DEFAULT TRUE,

    metadata JSONB NOT NULL DEFAULT '{}',

    PRIMARY KEY(worker_id, capability)
);

CREATE INDEX idx_worker_capabilities_lookup
ON worker_capabilities(capability, enabled);

CREATE TABLE task_leases (
    id UUID PRIMARY KEY,

    task_id UUID NOT NULL REFERENCES tasks(id),
    worker_id UUID NOT NULL REFERENCES workers(id),

    lease_token_hash VARCHAR(128) NOT NULL,

    generation BIGINT NOT NULL DEFAULT 1,

    status VARCHAR(32) NOT NULL DEFAULT 'active',

    acquired_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    renewed_at TIMESTAMPTZ,

    released_at TIMESTAMPTZ,

    UNIQUE(task_id, generation)
);

CREATE INDEX idx_task_leases_active
ON task_leases(expires_at)
WHERE status = 'active';

CREATE INDEX idx_task_leases_worker
ON task_leases(worker_id, status);

CREATE TABLE assets (
    id UUID PRIMARY KEY,

    run_id UUID NOT NULL REFERENCES runs(id),

    asset_type VARCHAR(64) NOT NULL,
    asset_key VARCHAR(512) NOT NULL,

    attributes JSONB NOT NULL DEFAULT '{}',

    first_seen_at TIMESTAMPTZ DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ DEFAULT NOW(),

    created_at TIMESTAMPTZ DEFAULT NOW(),

    UNIQUE(run_id, asset_type, asset_key)
);

CREATE INDEX idx_assets_run_type
ON assets(run_id, asset_type);

CREATE TABLE facts (
    id UUID PRIMARY KEY,

    run_id UUID NOT NULL REFERENCES runs(id),
    asset_id UUID REFERENCES assets(id),

    fact_type VARCHAR(100) NOT NULL,

    content JSONB NOT NULL,

    confidence NUMERIC(4,3),

    source_worker_id UUID REFERENCES workers(id),
    source_task_id UUID REFERENCES tasks(id),

    status VARCHAR(32) NOT NULL DEFAULT 'observed',

    observed_at TIMESTAMPTZ DEFAULT NOW(),
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_facts_run_type
ON facts(run_id, fact_type);

CREATE INDEX idx_facts_asset
ON facts(asset_id);

CREATE TABLE evidences (
    id UUID PRIMARY KEY,

    run_id UUID NOT NULL REFERENCES runs(id),

    fact_id UUID REFERENCES facts(id),
    task_id UUID REFERENCES tasks(id),

    evidence_type VARCHAR(64) NOT NULL,

    content_ref TEXT NOT NULL,

    content_hash VARCHAR(128),

    metadata JSONB NOT NULL DEFAULT '{}',

    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_evidences_run
ON evidences(run_id);

CREATE INDEX idx_evidences_fact
ON evidences(fact_id);

CREATE TABLE task_proposals (
    id UUID PRIMARY KEY,

    run_id UUID NOT NULL REFERENCES runs(id),

    worker_id UUID NOT NULL REFERENCES workers(id),
    parent_task_id UUID REFERENCES tasks(id),

    reason TEXT NOT NULL,

    proposed_task JSONB NOT NULL,

    status VARCHAR(32) NOT NULL DEFAULT 'pending',

    review_reason TEXT,

    reviewed_by VARCHAR(100),
    reviewed_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_proposals_pending
ON task_proposals(run_id, status, created_at);

CREATE TABLE findings (
    id UUID PRIMARY KEY,

    run_id UUID NOT NULL REFERENCES runs(id),

    title VARCHAR(255) NOT NULL,
    description TEXT,

    severity VARCHAR(32),
    status VARCHAR(32) NOT NULL DEFAULT 'unverified',

    confidence NUMERIC(4,3),

    source_task_id UUID REFERENCES tasks(id),

    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_findings_run_status
ON findings(run_id, status);

CREATE TABLE execution_events (
    id UUID PRIMARY KEY,

    run_id UUID NOT NULL REFERENCES runs(id),

    worker_id UUID REFERENCES workers(id),
    task_id UUID REFERENCES tasks(id),

    event_type VARCHAR(100) NOT NULL,

    payload JSONB NOT NULL DEFAULT '{}',

    sequence BIGINT GENERATED ALWAYS AS IDENTITY,

    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_execution_events_run
ON execution_events(run_id, created_at);

CREATE INDEX idx_execution_events_task
ON execution_events(task_id, created_at);

CREATE TABLE outbox_events (
    id UUID PRIMARY KEY,

    aggregate_type VARCHAR(64) NOT NULL,
    aggregate_id UUID NOT NULL,

    event_type VARCHAR(100) NOT NULL,
    payload JSONB NOT NULL,

    status VARCHAR(32) NOT NULL DEFAULT 'pending',

    attempts INT NOT NULL DEFAULT 0,

    created_at TIMESTAMPTZ DEFAULT NOW(),
    published_at TIMESTAMPTZ
);

CREATE INDEX idx_outbox_pending
ON outbox_events(created_at)
WHERE status = 'pending';

ALTER TABLE workers ADD COLUMN run_id UUID NOT NULL REFERENCES runs(id);
ALTER TABLE workers ADD COLUMN token_hash VARCHAR(128) NOT NULL;
ALTER TABLE task_leases ADD COLUMN submission_hash VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE findings ADD COLUMN evidence_refs JSONB NOT NULL DEFAULT '[]';
CREATE UNIQUE INDEX idx_one_active_lease ON task_leases(task_id) WHERE status='active';
CREATE UNIQUE INDEX idx_event_sequence ON execution_events(sequence);
CREATE INDEX idx_workers_run ON workers(run_id);
ALTER TABLE runs ADD CONSTRAINT runs_budget CHECK(max_workers BETWEEN 1 AND 64 AND max_tasks BETWEEN 1 AND 1000);
ALTER TABLE workers ADD CONSTRAINT workers_capacity CHECK(max_concurrency BETWEEN 1 AND 64 AND active_tasks BETWEEN 0 AND max_concurrency);
ALTER TABLE tasks ADD CONSTRAINT tasks_priority CHECK(priority BETWEEN 0 AND 100);
