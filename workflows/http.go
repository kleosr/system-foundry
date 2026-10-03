package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"

	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"

	"systemsfoundry/auth"
	"systemsfoundry/platform"
)

const TaskQueue = "onboarding"

var tenantPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type WorkflowClient interface {
	ExecuteWorkflow(ctx context.Context, options client.StartWorkflowOptions, workflow any, args ...any) (client.WorkflowRun, error)
	SignalWorkflow(ctx context.Context, workflowID, runID, signalName string, arg any) error
}

type Onboarding struct {
	client WorkflowClient
}

func NewOnboarding(workflowClient WorkflowClient) *Onboarding {
	return &Onboarding{client: workflowClient}
}

type startedView struct {
	WorkflowID string `json:"workflowId"`
	RunID      string `json:"runId"`
}

func WorkflowID(tenantID string) string {
	return "onboard-" + tenantID
}

func (o *Onboarding) ServeStart(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TenantID string `json:"tenantId"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || !tenantPattern.MatchString(body.TenantID) {
		_ = platform.WriteResult(w, platform.FailFields[startedView]("Invalid tenant.", map[string][]string{"tenantId": {"must match ^[A-Za-z0-9_-]{1,64}$"}}))
		return
	}
	run, err := o.client.ExecuteWorkflow(r.Context(), client.StartWorkflowOptions{
		ID:                                       WorkflowID(body.TenantID),
		TaskQueue:                                TaskQueue,
		WorkflowExecutionErrorWhenAlreadyStarted: true,
	}, Onboard, Input{TenantID: body.TenantID})
	var started *serviceerror.WorkflowExecutionAlreadyStarted
	if errors.As(err, &started) {
		_ = platform.WriteResult(w, platform.Fail[startedView](platform.CodeConflict, "Onboarding already started."))
		return
	}
	if err != nil {
		_ = platform.WriteResult(w, platform.Fail[startedView](platform.CodeUnavailable, "Workflow service unavailable."))
		return
	}
	_ = platform.WriteStatus(w, http.StatusAccepted, platform.Succeed(startedView{WorkflowID: run.GetID(), RunID: run.GetRunID()}))
}

func (o *Onboarding) ServeApprove(w http.ResponseWriter, r *http.Request) {
	actor, ok := auth.ActorFrom(r.Context())
	tenantID := r.PathValue("tenant")
	if !ok || !tenantPattern.MatchString(tenantID) {
		_ = platform.WriteResult(w, platform.Fail[bool](platform.CodeValidation, "Invalid tenant."))
		return
	}
	err := o.client.SignalWorkflow(r.Context(), WorkflowID(tenantID), "", SignalApprove, Approval{By: actor.Subject})
	var missing *serviceerror.NotFound
	if errors.As(err, &missing) {
		_ = platform.WriteResult(w, platform.Fail[bool](platform.CodeNotFound, "No open onboarding for this tenant."))
		return
	}
	if err != nil {
		_ = platform.WriteResult(w, platform.Fail[bool](platform.CodeUnavailable, "Workflow service unavailable."))
		return
	}
	_ = platform.WriteResult(w, platform.Succeed(true))
}
