package workflows

import (
	"testing"
	"time"
)

func TestApprovalBeforeWindowProvisions(t *testing.T) {
	start := time.Unix(1_700_000_000, 0).UTC()
	decision, err := Start("acme", start)
	if err != nil {
		t.Fatal(err)
	}
	decision = OnSignal(decision.State, true, start.Add(48*time.Hour))
	if decision.State.Step != StepProvision || len(decision.Effects) != 1 || decision.Effects[0] != EffectProvision {
		t.Fatalf("decision = %+v", decision)
	}
	decision = MarkProvisioned(decision.State)
	if decision.State.Step != StepDone {
		t.Fatal(decision.State.Step)
	}
}

func TestTickAtWindowExpires(t *testing.T) {
	start := time.Unix(1_700_000_000, 0).UTC()
	decision, err := Start("acme", start)
	if err != nil {
		t.Fatal(err)
	}
	decision = OnTick(decision.State, start.Add(ApprovalWindow))
	if decision.State.Step != StepExpired || decision.Effects[0] != EffectExpire {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestLateApprovalExpires(t *testing.T) {
	start := time.Unix(1_700_000_000, 0).UTC()
	decision, err := Start("acme", start)
	if err != nil {
		t.Fatal(err)
	}
	decision = OnSignal(decision.State, true, start.Add(ApprovalWindow))
	if decision.State.Step != StepExpired {
		t.Fatal(decision.State.Step)
	}
}

func TestRetryIncrementsAttempt(t *testing.T) {
	start := time.Unix(1_700_000_000, 0).UTC()
	decision, err := Start("acme", start)
	if err != nil {
		t.Fatal(err)
	}
	decision = OnSignal(decision.State, true, start.Add(time.Hour))
	decision = Retry(decision.State)
	if decision.State.Attempt != 2 || decision.State.Step != StepProvision {
		t.Fatalf("state = %+v", decision.State)
	}
}

func TestStartRejectsEmptyTenant(t *testing.T) {
	if _, err := Start("", time.Now()); err == nil {
		t.Fatal("expected error")
	}
}
