package workflows

import (
	"errors"
	"time"
)

const ApprovalWindow = 72 * time.Hour

type Step string

const (
	StepCreate    Step = "create"
	StepWait      Step = "wait_approval"
	StepProvision Step = "provision"
	StepDone      Step = "done"
	StepExpired   Step = "expired"
)

type Effect string

const (
	EffectCreateTenant Effect = "create_tenant"
	EffectProvision    Effect = "provision"
	EffectExpire       Effect = "expire"
)

type State struct {
	TenantID string
	Started  time.Time
	Step     Step
	Attempt  int
}

type Decision struct {
	State   State
	Effects []Effect
}

func Start(tenantID string, now time.Time) (Decision, error) {
	if tenantID == "" || now.IsZero() {
		return Decision{}, errors.New("tenant and start time are required")
	}
	return Decision{
		State:   State{TenantID: tenantID, Started: now, Step: StepWait, Attempt: 1},
		Effects: []Effect{EffectCreateTenant},
	}, nil
}

func OnSignal(state State, approved bool, now time.Time) Decision {
	if state.Step != StepWait {
		return Decision{State: state}
	}
	if !now.Before(state.Started.Add(ApprovalWindow)) || !approved {
		state.Step = StepExpired
		return Decision{State: state, Effects: []Effect{EffectExpire}}
	}
	state.Step = StepProvision
	return Decision{State: state, Effects: []Effect{EffectProvision}}
}

func OnTick(state State, now time.Time) Decision {
	if state.Step == StepWait && !now.Before(state.Started.Add(ApprovalWindow)) {
		state.Step = StepExpired
		return Decision{State: state, Effects: []Effect{EffectExpire}}
	}
	return Decision{State: state}
}

func MarkProvisioned(state State) Decision {
	if state.Step != StepProvision {
		return Decision{State: state}
	}
	state.Step = StepDone
	return Decision{State: state}
}

func Retry(state State) Decision {
	if state.Step != StepProvision && state.Step != StepWait {
		return Decision{State: state}
	}
	state.Attempt++
	return Decision{State: state}
}
