CREATE TABLE meta (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  store_id TEXT NOT NULL,
  backend TEXT NOT NULL,
  config_version BIGINT NOT NULL,
  epoch BIGINT NOT NULL,
  last_run_id TEXT NOT NULL DEFAULT '',
  last_started_at TIMESTAMPTZ,
  maintenance BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE TABLE accesses (
  access_id TEXT PRIMARY KEY,
  enabled BOOLEAN NOT NULL,
  spki_pins JSONB NOT NULL,
  max_exposures INTEGER NOT NULL,
  labels JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE consumer_tokens (
  token_id TEXT PRIMARY KEY,
  subject TEXT NOT NULL,
  scope TEXT NOT NULL CHECK (scope IN ('consumer', 'admin')),
  secret_sha256 BYTEA NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  revoked_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX consumer_tokens_expires_at ON consumer_tokens (expires_at);

CREATE TABLE policies (
  policy_id TEXT PRIMARY KEY,
  subject TEXT NOT NULL,
  exposure_selector TEXT NOT NULL,
  actions JSONB NOT NULL,
  enabled BOOLEAN NOT NULL,
  updated_version BIGINT NOT NULL
);
CREATE INDEX policies_subject ON policies (subject);

CREATE TABLE exposures (
  exposure_id TEXT PRIMARY KEY,
  access_id TEXT NOT NULL REFERENCES accesses (access_id) ON DELETE CASCADE,
  revision TEXT NOT NULL,
  protocol TEXT NOT NULL,
  transport_profile TEXT NOT NULL,
  admission_mode TEXT NOT NULL,
  description TEXT NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX exposures_access_id ON exposures (access_id);

CREATE TABLE audit (
  seq BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  at TIMESTAMPTZ NOT NULL,
  actor TEXT NOT NULL,
  action TEXT NOT NULL,
  object_type TEXT NOT NULL,
  object_id TEXT NOT NULL,
  config_version BIGINT NOT NULL,
  detail JSONB NOT NULL
);
CREATE INDEX audit_at ON audit (at);
