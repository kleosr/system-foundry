package search

import "sort"

func Fuse(lists [][]string, k int) []string {
	if k < 1 {
		k = 60
	}
	type acc struct {
		score float64
	}
	scores := map[string]acc{}
	order := make([]string, 0)
	for _, list := range lists {
		for rank, id := range list {
			current, ok := scores[id]
			if !ok {
				order = append(order, id)
			}
			current.score += 1 / float64(k+rank+1)
			scores[id] = current
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		return scores[order[i]].score > scores[order[j]].score
	})
	return order
}

func Page(fused []string, cursor string, limit int) (ids []string, next string) {
	if limit < 1 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	start := 0
	if cursor != "" {
		found := false
		for i, id := range fused {
			if id == cursor {
				start = i + 1
				found = true
				break
			}
		}
		if !found {
			return nil, ""
		}
	}
	end := start + limit
	if end > len(fused) {
		end = len(fused)
	}
	ids = append([]string(nil), fused[start:end]...)
	if end < len(fused) && len(ids) > 0 {
		next = ids[len(ids)-1]
	}
	return ids, next
}
