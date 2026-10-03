package gateway

import (
	"context"
	"testing"
	"time"
)

type scriptModel struct {
	name   string
	status int
	text   string
	tokens int
	delay  time.Duration
	calls  int
}

func (m *scriptModel) Name() string { return m.name }

func (m *scriptModel) Complete(ctx context.Context, prompt string, maxTokens int) (string, int, error) {
	m.calls++
	if m.delay > 0 {
		timer := time.NewTimer(m.delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", 0, ctx.Err()
		case <-timer.C:
		}
	}
	if m.status != 0 {
		return "", 0, &StatusError{Status: m.status}
	}
	return m.text, m.tokens, nil
}

type usageLog struct {
	rows []Usage
}

func (l *usageLog) Record(usage Usage) { l.rows = append(l.rows, usage) }

func TestCacheHitSkipsModelAndStaysInTenant(t *testing.T) {
	primary := &scriptModel{name: "primary", text: "answer", tokens: 4}
	cheap := &scriptModel{name: "cheap", text: "cheap", tokens: 2}
	proxy := NewProxy(primary, cheap, NewCache(10), NewBudget(1000), nil)
	first := proxy.Complete(context.Background(), Request{TenantID: "acme", Prompt: "red shoes"})
	if !first.OK || first.Value.CacheHit || primary.calls != 1 {
		t.Fatalf("first = %+v calls=%d", first, primary.calls)
	}
	second := proxy.Complete(context.Background(), Request{TenantID: "acme", Prompt: "red shoes"})
	if !second.OK || !second.Value.CacheHit || primary.calls != 1 {
		t.Fatalf("second = %+v calls=%d", second, primary.calls)
	}
	other := proxy.Complete(context.Background(), Request{TenantID: "beta", Prompt: "red shoes"})
	if !other.OK || other.Value.CacheHit || primary.calls != 2 {
		t.Fatalf("other = %+v calls=%d", other, primary.calls)
	}
}

func TestPrimary503UsesCheapModelOnce(t *testing.T) {
	primary := &scriptModel{name: "primary", status: 503}
	cheap := &scriptModel{name: "cheap", text: "fallback", tokens: 3}
	proxy := NewProxy(primary, cheap, NewCache(10), NewBudget(1000), nil)
	outcome := proxy.Complete(context.Background(), Request{TenantID: "acme", Prompt: "hello"})
	if !outcome.OK || outcome.Value.Model != "cheap" || outcome.Value.Text != "fallback" {
		t.Fatalf("outcome = %+v", outcome)
	}
	if primary.calls != 1 || cheap.calls != 1 {
		t.Fatalf("primary=%d cheap=%d", primary.calls, cheap.calls)
	}
}

func TestPrimary400DoesNotFailover(t *testing.T) {
	primary := &scriptModel{name: "primary", status: 400}
	cheap := &scriptModel{name: "cheap", text: "fallback", tokens: 3}
	proxy := NewProxy(primary, cheap, NewCache(10), NewBudget(1000), nil)
	outcome := proxy.Complete(context.Background(), Request{TenantID: "acme", Prompt: "hello"})
	if outcome.OK || cheap.calls != 0 || primary.calls != 1 {
		t.Fatalf("ok=%v primary=%d cheap=%d", outcome.OK, primary.calls, cheap.calls)
	}
}

func TestBudgetDenialSkipsModel(t *testing.T) {
	primary := &scriptModel{name: "primary", text: "answer", tokens: 4}
	proxy := NewProxy(primary, primary, NewCache(10), NewBudget(1), nil)
	outcome := proxy.Complete(context.Background(), Request{TenantID: "acme", Prompt: "this prompt is definitely over one token"})
	if outcome.OK || primary.calls != 0 {
		t.Fatalf("ok=%v calls=%d err=%v", outcome.OK, primary.calls, outcome.Error)
	}
}

func TestModelTimeoutFailsOver(t *testing.T) {
	primary := &scriptModel{name: "primary", delay: time.Second, text: "slow"}
	cheap := &scriptModel{name: "cheap", text: "fast", tokens: 1}
	proxy := NewProxy(primary, cheap, NewCache(10), NewBudget(1000), nil)
	proxy.modelTimeout = 20 * time.Millisecond
	outcome := proxy.Complete(context.Background(), Request{TenantID: "acme", Prompt: "hello"})
	if !outcome.OK || outcome.Value.Model != "cheap" {
		t.Fatalf("outcome = %+v err=%v", outcome.Value, outcome.Error)
	}
}

func TestUsageRecordsCacheHit(t *testing.T) {
	primary := &scriptModel{name: "primary", text: "answer", tokens: 4}
	log := &usageLog{}
	proxy := NewProxy(primary, primary, NewCache(10), NewBudget(1000), log)
	_ = proxy.Complete(context.Background(), Request{TenantID: "acme", Prompt: "red shoes"})
	_ = proxy.Complete(context.Background(), Request{TenantID: "acme", Prompt: "red shoes"})
	if len(log.rows) != 2 || !log.rows[1].CacheHit || log.rows[1].Tokens != 0 {
		t.Fatalf("rows = %+v", log.rows)
	}
}
