package search

import (
	"context"
	"errors"
	"sync"

	"systemsfoundry/platform"
)

var ErrCatalogFull = errors.New("tenant catalog is full")

type MemoryRepo struct {
	mu   sync.Mutex
	docs map[string][]storedDoc
	cap  int
}

func NewMemoryRepo(cap int) *MemoryRepo {
	if cap < 1 {
		cap = 10000
	}
	return &MemoryRepo{docs: map[string][]storedDoc{}, cap: cap}
}

func (r *MemoryRepo) Save(_ context.Context, actor platform.Actor, doc Document, vector []float32) error {
	tenantID := actor.TenantID
	r.mu.Lock()
	defer r.mu.Unlock()
	rows := r.docs[tenantID]
	for i, row := range rows {
		if row.ID == doc.ID {
			rows[i] = storedDoc{Document: doc, Vector: append([]float32(nil), vector...)}
			r.docs[tenantID] = rows
			return nil
		}
	}
	if len(rows) >= r.cap {
		return ErrCatalogFull
	}
	copied := append([]float32(nil), vector...)
	r.docs[tenantID] = append(rows, storedDoc{Document: doc, Vector: copied})
	return nil
}

func (r *MemoryRepo) Rank(_ context.Context, actor platform.Actor, terms []string, vector []float32, depth int) ([]string, []string, error) {
	r.mu.Lock()
	rows := make([]storedDoc, len(r.docs[actor.TenantID]))
	copy(rows, r.docs[actor.TenantID])
	r.mu.Unlock()
	docs := make([]Document, 0, len(rows))
	for _, row := range rows {
		docs = append(docs, row.Document)
	}
	lexical := make([]string, 0, len(docs))
	for _, ranked := range BM25(docs, terms) {
		if ranked.Score > 0 && len(lexical) < depth {
			lexical = append(lexical, ranked.ID)
		}
	}
	semantic := rankCosine(rows, vector)
	if len(semantic) > depth {
		semantic = semantic[:depth]
	}
	return lexical, semantic, nil
}
