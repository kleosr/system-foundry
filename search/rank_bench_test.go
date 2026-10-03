package search

import (
	"strconv"
	"testing"
)

func BenchmarkBM25ThousandDocs(b *testing.B) {
	docs := make([]Document, 1000)
	for i := range docs {
		docs[i] = Analyze(strconv.Itoa(i), "dogs", "dogs and cats share a yard")
	}
	query := Tokens("dogs")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		BM25(docs, query)
	}
}
