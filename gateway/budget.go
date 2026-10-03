package gateway

import "sync"

type Budget struct {
	mu    sync.Mutex
	limit int
	used  map[string]int
}

func NewBudget(limit int) *Budget {
	if limit < 1 {
		limit = 100000
	}
	return &Budget{limit: limit, used: map[string]int{}}
}

func (b *Budget) Reserve(tenantID string, tokens int) bool {
	if tokens < 0 {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.used[tenantID]+tokens > b.limit {
		return false
	}
	b.used[tenantID] += tokens
	return true
}

func (b *Budget) Release(tenantID string, tokens int) {
	b.adjust(tenantID, -tokens)
}

func (b *Budget) Settle(tenantID string, reserved, actual int) {
	b.adjust(tenantID, actual-reserved)
}

func (b *Budget) adjust(tenantID string, delta int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	next := b.used[tenantID] + delta
	if next < 0 {
		next = 0
	}
	b.used[tenantID] = next
}

func EstimateTokens(prompt string) int {
	if prompt == "" {
		return 0
	}
	return (len(prompt) + 3) / 4
}
