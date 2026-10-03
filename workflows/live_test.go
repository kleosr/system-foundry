package workflows

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"

	"systemsfoundry/auth"
	"systemsfoundry/platform"
)

func TestLiveTemporalStartApproveProvision(t *testing.T) {
	if os.Getenv("FOUNDRY_TEST_TEMPORAL") != "1" {
		t.Skip("FOUNDRY_TEST_TEMPORAL is not 1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	server, err := testsuite.StartDevServer(ctx, testsuite.DevServerOptions{LogLevel: "error"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Stop() })
	temporalClient := server.Client()
	dir := &memoryDirectory{}
	runner := worker.New(temporalClient, TaskQueue, worker.Options{})
	runner.RegisterWorkflow(Onboard)
	runner.RegisterActivity(&Activities{Dir: dir})
	if err = runner.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runner.Stop)

	onboarding := NewOnboarding(temporalClient)
	rec := httptest.NewRecorder()
	onboarding.ServeStart(rec, httptest.NewRequest(http.MethodPost, "/ops/onboarding", strings.NewReader(`{"tenantId":"acme"}`)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("start = %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	onboarding.ServeStart(rec, httptest.NewRequest(http.MethodPost, "/ops/onboarding", strings.NewReader(`{"tenantId":"acme"}`)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate start = %d %s", rec.Code, rec.Body.String())
	}
	approve := httptest.NewRequest(http.MethodPost, "/ops/onboarding/acme/approve", nil)
	approve.SetPathValue("tenant", "acme")
	approve = approve.WithContext(auth.WithActor(approve.Context(), platform.Actor{Subject: "op-1", TenantID: "ops", Role: platform.RoleOwner}))
	rec = httptest.NewRecorder()
	onboarding.ServeApprove(rec, approve)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve = %d %s", rec.Code, rec.Body.String())
	}
	var state State
	if err = temporalClient.GetWorkflow(ctx, WorkflowID("acme"), "").Get(ctx, &state); err != nil {
		t.Fatal(err)
	}
	if state.Step != StepDone || len(dir.provisioned) != 1 {
		t.Fatalf("state=%s provisioned=%d", state.Step, len(dir.provisioned))
	}
}

func TestPostgresDirectoryTransitionsAreIdempotentAndFinal(t *testing.T) {
	dsn := os.Getenv("FOUNDRY_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("FOUNDRY_TEST_POSTGRES_DSN is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	dir := NewPostgresDirectory(pool)
	tenantID := "wf-" + time.Now().Format("150405.000000")
	tenantID = strings.ReplaceAll(tenantID, ".", "-")
	for i := 0; i < 2; i++ {
		if err = dir.CreateTenant(ctx, tenantID); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
		if err = dir.ProvisionTenant(ctx, tenantID); err != nil {
			t.Fatalf("provision %d: %v", i, err)
		}
	}
	if err = dir.ExpireTenant(ctx, tenantID); err == nil {
		t.Fatal("active tenant was expired")
	}
	if err = dir.ProvisionTenant(ctx, "never-created"); err == nil {
		t.Fatal("missing tenant was provisioned")
	}
}
