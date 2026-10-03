package search

import (
	"os"
	"strings"
	"testing"
)

func TestMigrationForcesTenantRLSAndReadOnlyAgent(t *testing.T) {
	sqlBytes, err := os.ReadFile("sql/20261002_documents.sql")
	if err != nil {
		t.Fatal(err)
	}
	text := string(sqlBytes)
	for _, needle := range []string{
		"ENABLE ROW LEVEL SECURITY",
		"FORCE ROW LEVEL SECURITY",
		"current_setting('app.tenant_id', true)",
		"GRANT SELECT ON documents, document_terms, tenants TO agent_reader",
		"CREATE ROLE agent_reader NOLOGIN",
	} {
		if !strings.Contains(text, needle) {
			t.Fatalf("missing %s", needle)
		}
	}
	if strings.Contains(text, "GRANT INSERT") && strings.Contains(text, "TO agent_reader") {
		t.Fatal("agent_reader received a write grant")
	}
}
