package search

import (
	"context"
	"testing"

	"systemsfoundry/platform"
)

func TestFindStaysInsideActorTenant(t *testing.T) {
	repo := NewMemoryRepo(10)
	svc := NewService(repo)
	owner := platform.Actor{Subject: "owner-1", TenantID: "acme", Role: platform.RoleOwner}
	other := platform.Actor{Subject: "owner-2", TenantID: "beta", Role: platform.RoleOwner}
	if outcome := svc.Index(context.Background(), owner, "d1", "dogs", "red dogs"); !outcome.OK {
		t.Fatal(outcome.Error)
	}
	if outcome := svc.Index(context.Background(), other, "d2", "dogs", "blue dogs"); !outcome.OK {
		t.Fatal(outcome.Error)
	}
	found := svc.Find(context.Background(), owner, "dogs", "", 10)
	if !found.OK {
		t.Fatal(found.Error)
	}
	for _, id := range found.Value.IDs {
		if id == "d2" {
			t.Fatal("other tenant document was returned")
		}
	}
	if len(found.Value.IDs) != 1 || found.Value.IDs[0] != "d1" {
		t.Fatalf("ids = %v", found.Value.IDs)
	}
}

func TestAgentCannotIndex(t *testing.T) {
	svc := NewService(NewMemoryRepo(10))
	agent := platform.Actor{Subject: "agent-1", TenantID: "acme", Role: platform.RoleAgent}
	outcome := svc.Index(context.Background(), agent, "d1", "dogs", "red dogs")
	if outcome.OK || outcome.Error.Code != platform.CodeForbidden {
		t.Fatalf("outcome = %+v", outcome.Error)
	}
	owner := platform.Actor{Subject: "owner-1", TenantID: "acme", Role: platform.RoleOwner}
	found := svc.Find(context.Background(), owner, "dogs", "", 10)
	if !found.OK || len(found.Value.IDs) != 0 {
		t.Fatalf("ids = %+v", found)
	}
}

func TestEmptyQueryIsRejected(t *testing.T) {
	svc := NewService(NewMemoryRepo(10))
	owner := platform.Actor{Subject: "owner-1", TenantID: "acme", Role: platform.RoleOwner}
	outcome := svc.Find(context.Background(), owner, "", "", 10)
	if outcome.OK || outcome.Error.Code != platform.CodeValidation {
		t.Fatalf("outcome = %+v", outcome.Error)
	}
}
