package platform

import (
	"hash/fnv"
	"math"
	"strings"
)

const EmbedDims = 256

func HashEmbed(text string) []float32 {
	vec := make([]float32, EmbedDims)
	for _, word := range strings.Fields(strings.ToLower(text)) {
		hasher := fnv.New64a()
		_, _ = hasher.Write([]byte(word))
		sum := hasher.Sum64()
		idx := int(sum % uint64(EmbedDims))
		sign := float32(1)
		if sum&1 == 1 {
			sign = -1
		}
		vec[idx] += sign
	}
	return normalize(vec)
}

func Cosine(left, right []float32) float64 {
	if len(left) == 0 || len(left) != len(right) {
		return 0
	}
	var dot, leftNorm, rightNorm float64
	for i := range left {
		lv := float64(left[i])
		rv := float64(right[i])
		dot += lv * rv
		leftNorm += lv * lv
		rightNorm += rv * rv
	}
	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}
	return dot / (math.Sqrt(leftNorm) * math.Sqrt(rightNorm))
}

func normalize(vec []float32) []float32 {
	var sum float64
	for _, n := range vec {
		sum += float64(n) * float64(n)
	}
	if sum == 0 {
		return vec
	}
	scale := float32(1 / math.Sqrt(sum))
	for i := range vec {
		vec[i] *= scale
	}
	return vec
}
