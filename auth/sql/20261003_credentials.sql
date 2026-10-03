CREATE TABLE users (
    user_id text PRIMARY KEY,
    email text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    tenant_id text NOT NULL CHECK (tenant_id ~ '^[A-Za-z0-9_-]{1,64}$'),
    role text NOT NULL CHECK (role IN ('owner', 'member', 'agent')),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    session_hash text PRIMARY KEY,
    user_id text NOT NULL REFERENCES users (user_id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL
);

CREATE TABLE oauth_clients (
    client_id text PRIMARY KEY,
    audience text NOT NULL,
    redirects text[] NOT NULL CHECK (cardinality(redirects) > 0)
);

CREATE TABLE auth_codes (
    code_hash text PRIMARY KEY,
    user_id text NOT NULL REFERENCES users (user_id) ON DELETE CASCADE,
    client_id text NOT NULL REFERENCES oauth_clients (client_id) ON DELETE CASCADE,
    redirect_uri text NOT NULL,
    challenge text NOT NULL,
    nonce text NOT NULL,
    expires_at timestamptz NOT NULL,
    used boolean NOT NULL DEFAULT false
);

CREATE TABLE refresh_tokens (
    token_hash text PRIMARY KEY,
    family text NOT NULL,
    user_id text NOT NULL REFERENCES users (user_id) ON DELETE CASCADE,
    client_id text NOT NULL REFERENCES oauth_clients (client_id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    used boolean NOT NULL DEFAULT false
);

CREATE INDEX sessions_user ON sessions (user_id);
CREATE INDEX refresh_tokens_family ON refresh_tokens (family);
CREATE INDEX refresh_tokens_user ON refresh_tokens (user_id);

DO $$
BEGIN
    CREATE ROLE auth_service NOLOGIN;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
GRANT USAGE ON SCHEMA public TO auth_service;
GRANT SELECT, INSERT, UPDATE, DELETE ON users, sessions, oauth_clients, auth_codes, refresh_tokens TO auth_service;
GRANT auth_service TO foundry_app;
