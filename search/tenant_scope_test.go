package search

import (
	"strings"
	"testing"
)

func TestTenantScopeIsLocalToTheTransaction(t *testing.T) {
	if strings.Contains(SetTenantSQL, "%s") || strings.Contains(SetTenantSQL, "+") {
		t.Fatal("tenant id must stay a parameter")
	}
	if !strings.Contains(SetTenantSQL, "set_config") || !strings.HasSuffix(SetTenantSQL, "true)") {
		t.Fatalf("sql = %s", SetTenantSQL)
	}
}
