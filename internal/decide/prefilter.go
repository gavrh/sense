package decide

import (
	"math"
	"sort"
	"strings"
	"unicode"

	"gavrh/sense/internal/serp"
)

const (
	bm25K1 = 1.2
	bm25B  = 0.75
)

// Prefilter returns indices of the topN results by BM25 score against query, best first.
// If topN <= 0 it returns every index. Ties break by original index.
func Prefilter(query string, results []serp.Result, topN int) []int {
	if len(results) == 0 {
		return nil
	}

	documents := make([][]string, len(results))
	lengths := make([]int, len(results))
	var totalLength int
	for i, result := range results {
		documents[i] = tokenize(result.Title + " " + result.Snippet)
		lengths[i] = len(documents[i])
		totalLength += lengths[i]
	}

	queryTerms := tokenize(query)
	avgLength := float64(totalLength) / float64(len(results))
	if avgLength == 0 {
		avgLength = 1
	}

	documentFreq := make(map[string]int, len(queryTerms))
	for _, term := range queryTerms {
		for _, document := range documents {
			if termFrequency(document, term) > 0 {
				documentFreq[term]++
			}
		}
	}

	scores := make([]float64, len(results))
	for i, document := range documents {
		scores[i] = bm25Score(document, lengths[i], queryTerms, documentFreq, len(results), avgLength)
	}

	indices := make([]int, len(results))
	for i := range indices {
		indices[i] = i
	}
	sort.SliceStable(indices, func(a, b int) bool {
		if scores[indices[a]] != scores[indices[b]] {
			return scores[indices[a]] > scores[indices[b]]
		}
		return indices[a] < indices[b]
	})

	if topN > 0 && topN < len(indices) {
		indices = indices[:topN]
	}
	return indices
}

func bm25Score(document []string, length int, queryTerms []string, documentFreq map[string]int, total int, avgLength float64) float64 {
	var score float64
	for _, term := range queryTerms {
		frequency := termFrequency(document, term)
		if frequency == 0 {
			continue
		}

		idf := math.Log(1 + (float64(total-documentFreq[term])+0.5)/(float64(documentFreq[term])+0.5))
		numerator := float64(frequency) * (bm25K1 + 1)
		denominator := float64(frequency) + bm25K1*(1-bm25B+bm25B*float64(length)/avgLength)
		score += idf * numerator / denominator
	}
	return score
}

func termFrequency(document []string, term string) int {
	count := 0
	for _, token := range document {
		if token == term {
			count++
		}
	}
	return count
}

func tokenize(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}
