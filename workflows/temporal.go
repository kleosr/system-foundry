package workflows

import (
	"context"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const SignalApprove = "approve"

type Input struct {
	TenantID string
}

type Approval struct {
	By string
}

type Directory interface {
	CreateTenant(ctx context.Context, tenantID string) error
	ProvisionTenant(ctx context.Context, tenantID string) error
	ExpireTenant(ctx context.Context, tenantID string) error
}

type Activities struct {
	Dir Directory
}

func (a *Activities) CreateTenant(ctx context.Context, tenantID string) error {
	return a.Dir.CreateTenant(ctx, tenantID)
}

func (a *Activities) ProvisionTenant(ctx context.Context, tenantID string) error {
	return a.Dir.ProvisionTenant(ctx, tenantID)
}

func (a *Activities) ExpireTenant(ctx context.Context, tenantID string) error {
	return a.Dir.ExpireTenant(ctx, tenantID)
}

func Onboard(ctx workflow.Context, in Input) (State, error) {
	_ = workflow.GetVersion(ctx, "onboard-v1", workflow.DefaultVersion, 1)
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2,
			MaximumAttempts:    5,
		},
	})
	decision, err := Start(in.TenantID, workflow.Now(ctx))
	if err != nil {
		return State{}, err
	}
	state := decision.State
	if err = workflow.ExecuteActivity(ctx, "CreateTenant", in.TenantID).Get(ctx, nil); err != nil {
		return state, err
	}
	signalCh := workflow.GetSignalChannel(ctx, SignalApprove)
	timerCtx, cancel := workflow.WithCancel(ctx)
	timer := workflow.NewTimer(timerCtx, ApprovalWindow)
	var approval Approval
	var timedOut bool
	selector := workflow.NewSelector(ctx)
	selector.AddReceive(signalCh, func(channel workflow.ReceiveChannel, more bool) {
		channel.Receive(ctx, &approval)
		cancel()
	})
	selector.AddFuture(timer, func(future workflow.Future) {
		_ = future.Get(timerCtx, nil)
		timedOut = true
	})
	selector.Select(ctx)
	next := OnSignal(state, approval.By != "" && !timedOut, workflow.Now(ctx))
	state = next.State
	switch state.Step {
	case StepExpired:
		err = workflow.ExecuteActivity(ctx, "ExpireTenant", in.TenantID).Get(ctx, nil)
	case StepProvision:
		err = workflow.ExecuteActivity(ctx, "ProvisionTenant", in.TenantID).Get(ctx, nil)
		if err == nil {
			state = MarkProvisioned(state).State
		}
	}
	return state, err
}
