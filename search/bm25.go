package search

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

type Document struct {
	ID     string
	Title  string
	Body   string
	Terms  map[string]int
	Length int
}

type Ranked struct {
	ID    string
	Score float64
}

func Tokens(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
}

func Analyze(id, title, body string) Document {
	terms := map[string]int{}
	tokens := Tokens(title + " " + body)
	for _, token := range tokens {
		terms[token]++
	}
	return Document{ID: id, Title: title, Body: body, Terms: terms, Length: len(tokens)}
}

func BM25(docs []Document, query []string) []Ranked {
	if len(docs) == 0 || len(query) == 0 {
		return nil
	}
	n := float64(len(docs))
	var totalLen int
	df := map[string]int{}
	querySet := map[string]struct{}{}
	for _, term := range query {
		querySet[term] = struct{}{}
	}
	for _, doc := range docs {
		totalLen += doc.Length
		for term := range doc.Terms {
			if _, wanted := querySet[term]; wanted {
				df[term]++
			}
		}
	}
	avg := float64(totalLen) / n
	if avg == 0 {
		avg = 1
	}
	const k1 = 1.2
	const b = 0.75
	ranked := make([]Ranked, 0, len(docs))
	for _, doc := range docs {
		dl := float64(doc.Length)
		if dl == 0 {
			dl = 1
		}
		var score float64
		for _, term := range query {
			freq := float64(doc.Terms[term])
			if freq == 0 {
				continue
			}
			dfi := float64(df[term])
			idf := math.Log(1 + (n-dfi+0.5)/(dfi+0.5))
			denom := freq + k1*(1-b+b*dl/avg)
			score += idf * (freq * (k1 + 1) / denom)
		}
		ranked = append(ranked, Ranked{ID: doc.ID, Score: score})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Score == ranked[j].Score {
			return ranked[i].ID < ranked[j].ID
		}
		return ranked[i].Score > ranked[j].Score
	})
	return ranked
}
