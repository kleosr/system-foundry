CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE tenants (
    tenant_id text PRIMARY KEY CHECK (tenant_id ~ '^[A-Za-z0-9_-]{1,64}$'),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'active', 'expired')),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE documents (
    tenant_id text NOT NULL REFERENCES tenants (tenant_id),
    document_id text NOT NULL CHECK (length(document_id) BETWEEN 1 AND 128),
    title text NOT NULL,
    body text NOT NULL,
    embedding vector(256) NOT NULL,
    term_count integer NOT NULL CHECK (term_count >= 0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, document_id)
);

CREATE TABLE document_terms (
    tenant_id text NOT NULL,
    document_id text NOT NULL,
    term text NOT NULL,
    freq integer NOT NULL CHECK (freq > 0),
    PRIMARY KEY (tenant_id, document_id, term),
    FOREIGN KEY (tenant_id, document_id) REFERENCES documents (tenant_id, document_id) ON DELETE CASCADE
);

CREATE INDEX documents_embedding_hnsw ON documents USING hnsw (embedding vector_cosine_ops);
CREATE INDEX document_terms_tenant_term ON document_terms (tenant_id, term);

ALTER TABLE tenants ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenants FORCE ROW LEVEL SECURITY;
ALTER TABLE documents ENABLE ROW LEVEL SECURITY;
ALTER TABLE documents FORCE ROW LEVEL SECURITY;
ALTER TABLE document_terms ENABLE ROW LEVEL SECURITY;
ALTER TABLE document_terms FORCE ROW LEVEL SECURITY;

CREATE POLICY tenants_tenant ON tenants
    USING (
        current_setting('app.tenant_id', true) <> ''
        AND tenant_id = current_setting('app.tenant_id', true)
    )
    WITH CHECK (
        current_setting('app.tenant_id', true) <> ''
        AND tenant_id = current_setting('app.tenant_id', true)
    );

CREATE POLICY documents_tenant ON documents
    USING (
        current_setting('app.tenant_id', true) <> ''
        AND tenant_id = current_setting('app.tenant_id', true)
    )
    WITH CHECK (
        current_setting('app.tenant_id', true) <> ''
        AND tenant_id = current_setting('app.tenant_id', true)
    );

CREATE POLICY document_terms_tenant ON document_terms
    USING (
        current_setting('app.tenant_id', true) <> ''
        AND tenant_id = current_setting('app.tenant_id', true)
    )
    WITH CHECK (
        current_setting('app.tenant_id', true) <> ''
        AND tenant_id = current_setting('app.tenant_id', true)
    );

DO $$
BEGIN
    CREATE ROLE app_writer NOLOGIN;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
GRANT USAGE ON SCHEMA public TO app_writer;
GRANT SELECT, INSERT, UPDATE, DELETE ON documents, document_terms, tenants TO app_writer;

DO $$
BEGIN
    CREATE ROLE agent_reader NOLOGIN;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
GRANT USAGE ON SCHEMA public TO agent_reader;
GRANT SELECT ON documents, document_terms, tenants TO agent_reader;

DO $$
BEGIN
    CREATE ROLE foundry_app NOLOGIN NOINHERIT;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
GRANT app_writer, agent_reader TO foundry_app;
