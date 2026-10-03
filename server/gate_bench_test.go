package server

import (
	"context"
	"testing"
)

func BenchmarkGateEnterLeave(b *testing.B) {
	gate, err := NewGate(1024, 128)
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err = gate.Enter(ctx); err != nil {
			b.Fatal(err)
		}
		gate.Leave()
	}
}
