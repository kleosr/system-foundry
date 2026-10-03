package auth

import (
	"testing"

	"systemsfoundry/platform"
)

func TestMissingActorIsDenied(t *testing.T) {
	if Can(platform.Actor{}, ActionRead, "acme") {
		t.Fatal("empty actor was allowed")
	}
}

func TestOtherTenantCannotReadOrWrite(t *testing.T) {
	actor := platform.Actor{Subject: "user-1", TenantID: "acme", Role: platform.RoleOwner}
	if Can(actor, ActionRead, "beta") || Can(actor, ActionWrite, "beta") {
		t.Fatal("cross-tenant access was allowed")
	}
}

func TestOperatorNeedsOwnerOfOpsTenant(t *testing.T) {
	cases := []struct {
		actor platform.Actor
		ops   string
		want  bool
	}{
		{platform.Actor{Subject: "u", TenantID: "ops", Role: platform.RoleOwner}, "ops", true},
		{platform.Actor{Subject: "u", TenantID: "ops", Role: platform.RoleMember}, "ops", false},
		{platform.Actor{Subject: "u", TenantID: "acme", Role: platform.RoleOwner}, "ops", false},
		{platform.Actor{Subject: "u", TenantID: "", Role: platform.RoleOwner}, "", false},
	}
	for _, tc := range cases {
		if got := IsOperator(tc.actor, tc.ops); got != tc.want {
			t.Fatalf("IsOperator(%+v, %q) = %v", tc.actor, tc.ops, got)
		}
	}
}

func TestDatabaseRoleKeepsAgentsReadOnly(t *testing.T) {
	if role, ok := DatabaseRoleFor(platform.Actor{Role: platform.RoleAgent}); !ok || role != DatabaseReader {
		t.Fatalf("agent role = %q %v", role, ok)
	}
	if role, ok := DatabaseRoleFor(platform.Actor{Role: platform.RoleMember}); !ok || role != DatabaseWriter {
		t.Fatalf("member role = %q %v", role, ok)
	}
	if _, ok := DatabaseRoleFor(platform.Actor{Role: "admin"}); ok {
		t.Fatal("unknown role got a database role")
	}
}

func TestAgentIsReadOnly(t *testing.T) {
	actor := platform.Actor{Subject: "agent-1", TenantID: "acme", Role: platform.RoleAgent}
	if !Can(actor, ActionRead, "acme") {
		t.Fatal("agent read denied")
	}
	if Can(actor, ActionWrite, "acme") {
		t.Fatal("agent write allowed")
	}
}

func TestOwnerCanWriteOwnTenant(t *testing.T) {
	actor := platform.Actor{Subject: "user-1", TenantID: "acme", Role: platform.RoleOwner}
	if !Can(actor, ActionRead, "acme") || !Can(actor, ActionWrite, "acme") {
		t.Fatal("owner denied")
	}
}

func TestUnknownRoleIsDenied(t *testing.T) {
	actor := platform.Actor{Subject: "user-1", TenantID: "acme", Role: "admin"}
	if Can(actor, ActionRead, "acme") {
		t.Fatal("unknown role allowed")
	}
}

func TestRedactMasksPIIAndSecrets(t *testing.T) {
	raw := "mail ada@example.com phone 415-555-1212 ssn 123-45-6789 card 4111111111111111 Bearer abc.def.ghi"
	masked := Redact(raw)
	for _, secret := range []string{"ada@example.com", "415-555-1212", "123-45-6789", "4111111111111111", "abc.def.ghi"} {
		if contains(masked, secret) {
			t.Fatalf("secret %q still in %q", secret, masked)
		}
	}
}

func contains(text, part string) bool {
	return len(part) > 0 && len(text) >= len(part) && (text == part || len(text) > 0 && (indexOf(text, part) >= 0))
}

func indexOf(text, part string) int {
	for i := 0; i+len(part) <= len(text); i++ {
		if text[i:i+len(part)] == part {
			return i
		}
	}
	return -1
}
