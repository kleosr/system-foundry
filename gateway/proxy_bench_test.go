package gateway

import (
	"context"
	"testing"
)

func BenchmarkCacheHit(b *testing.B) {
	primary := &scriptModel{name: "primary", text: "answer", tokens: 4}
	proxy := NewProxy(primary, primary, NewCache(1000), NewBudget(1_000_000_000), nil)
	req := Request{TenantID: "acme", Prompt: "red shoes for a long walk"}
	if outcome := proxy.Complete(context.Background(), req); !outcome.OK {
		b.Fatal(outcome.Error)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		outcome := proxy.Complete(context.Background(), req)
		if !outcome.OK || !outcome.Value.CacheHit {
			b.Fatal(outcome.Error)
		}
	}
}
