package workflows

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.temporal.io/sdk/testsuite"
)

type memoryDirectory struct {
	fails       int
	created     []string
	provisioned []string
	expired     []string
}

func (d *memoryDirectory) CreateTenant(context.Context, string) error {
	if d.fails > 0 {
		d.fails--
		return errors.New("temporary")
	}
	d.created = append(d.created, "created")
	return nil
}

func (d *memoryDirectory) ProvisionTenant(context.Context, string) error {
	d.provisioned = append(d.provisioned, "provisioned")
	return nil
}

func (d *memoryDirectory) ExpireTenant(context.Context, string) error {
	d.expired = append(d.expired, "expired")
	return nil
}

func TestOnboardExpiresWithoutApproval(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	dir := &memoryDirectory{}
	acts := &Activities{Dir: dir}
	env.RegisterWorkflow(Onboard)
	env.RegisterActivity(acts.CreateTenant)
	env.RegisterActivity(acts.ProvisionTenant)
	env.RegisterActivity(acts.ExpireTenant)
	env.ExecuteWorkflow(Onboard, Input{TenantID: "acme"})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	var state State
	if err := env.GetWorkflowResult(&state); err != nil {
		t.Fatal(err)
	}
	if state.Step != StepExpired || len(dir.expired) != 1 || len(dir.provisioned) != 0 {
		t.Fatalf("state=%s expired=%d provisioned=%d", state.Step, len(dir.expired), len(dir.provisioned))
	}
}

func TestOnboardProvisionsAfterApproval(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	dir := &memoryDirectory{}
	acts := &Activities{Dir: dir}
	env.RegisterWorkflow(Onboard)
	env.RegisterActivity(acts.CreateTenant)
	env.RegisterActivity(acts.ProvisionTenant)
	env.RegisterActivity(acts.ExpireTenant)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalApprove, Approval{By: "lead"})
	}, time.Hour)
	env.ExecuteWorkflow(Onboard, Input{TenantID: "acme"})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	var state State
	if err := env.GetWorkflowResult(&state); err != nil {
		t.Fatal(err)
	}
	if state.Step != StepDone || len(dir.provisioned) != 1 {
		t.Fatalf("state=%s provisioned=%d", state.Step, len(dir.provisioned))
	}
}

func TestCreateTenantRetriesThenSucceeds(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	dir := &memoryDirectory{fails: 2}
	acts := &Activities{Dir: dir}
	env.RegisterWorkflow(Onboard)
	env.RegisterActivity(acts.CreateTenant)
	env.RegisterActivity(acts.ProvisionTenant)
	env.RegisterActivity(acts.ExpireTenant)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalApprove, Approval{By: "lead"})
	}, time.Hour)
	env.ExecuteWorkflow(Onboard, Input{TenantID: "acme"})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	if len(dir.created) != 1 || dir.fails != 0 {
		t.Fatalf("created=%d fails=%d", len(dir.created), dir.fails)
	}
}
