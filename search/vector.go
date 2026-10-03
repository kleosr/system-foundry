package search

import (
	"sort"

	"systemsfoundry/platform"
)

type storedDoc struct {
	Document
	Vector []float32
}

func rankCosine(docs []storedDoc, queryVec []float32) []string {
	type pair struct {
		id    string
		score float64
	}
	pairs := make([]pair, 0, len(docs))
	for _, doc := range docs {
		pairs = append(pairs, pair{id: doc.ID, score: platform.Cosine(doc.Vector, queryVec)})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].score == pairs[j].score {
			return pairs[i].id < pairs[j].id
		}
		return pairs[i].score > pairs[j].score
	})
	ids := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		ids = append(ids, pair.id)
	}
	return ids
}
