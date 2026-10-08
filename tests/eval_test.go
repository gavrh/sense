package tests

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"gavrh/sense/internal/decide"
	"gavrh/sense/internal/jev"
	"gavrh/sense/internal/serp"
)

// evalMaxOptions mirrors the Decider default so the BM25 baseline is compared on
// the same candidate set the decider is given.
const evalMaxOptions = 8

// evalCase is one query and the URL substring a correct top-1 must contain.
// Add real cases by appending {"query": "...", "expect_url": "..."} to testdata/eval.json.
type evalCase struct {
	Query     string `json:"query"`
	ExpectURL string `json:"expect_url"`
}

// TestEvalAgainstBM25 is the go/no-go gate: Laya must beat a plain BM25 baseline.
func TestEvalAgainstBM25(t *testing.T) {
	serpURL := os.Getenv("SERP_URL")
	jevURL := os.Getenv("JEV_BASE_URL")
	if os.Getenv("SENSE_EVAL") != "1" || serpURL == "" || jevURL == "" {
		t.Skip("set SENSE_EVAL=1, SERP_URL and JEV_BASE_URL to run the eval")
	}

	cases := loadEvalCases(t)
	searcher, err := serp.New(serp.Options{Endpoint: serpURL})
	if err != nil {
		t.Fatalf("serp.New: %v", err)
	}

	model := os.Getenv("JEV_MODEL")
	decider := decide.New(jev.New(jevURL, os.Getenv("JEV_API_KEY"), model, 60*time.Second), decide.Options{Model: model, MaxOptions: evalMaxOptions})

	ctx := context.Background()
	var bm25Hits, layaHits, scored int

	for _, tc := range cases {
		results, err := searcher.Search(ctx, tc.Query)
		if err != nil {
			t.Errorf("%q: search: %v", tc.Query, err)
			continue
		}
		if len(results) == 0 {
			t.Logf("%q: no results", tc.Query)
			continue
		}

		top := decide.Prefilter(tc.Query, results, 1)
		bm25Hit := strings.Contains(results[top[0]].URL, tc.ExpectURL)

		subset := subsetResults(results, decide.Prefilter(tc.Query, results, evalMaxOptions))
		selection, err := decider.RankResults(ctx, tc.Query, subset)
		if err != nil {
			t.Errorf("%q: rank: %v", tc.Query, err)
			continue
		}
		layaHit := !selection.Abstained && strings.Contains(subset[selection.Index].URL, tc.ExpectURL)

		if bm25Hit {
			bm25Hits++
		}
		if layaHit {
			layaHits++
		}
		scored++

		t.Logf("%-45q bm25=%-5v laya=%-5v", tc.Query, bm25Hit, layaHit)
	}

	if scored == 0 {
		t.Skip("no scored queries")
	}

	bm25Accuracy := float64(bm25Hits) / float64(scored)
	layaAccuracy := float64(layaHits) / float64(scored)
	t.Logf("accuracy over %d queries: bm25=%.2f laya=%.2f", scored, bm25Accuracy, layaAccuracy)

	if layaAccuracy <= bm25Accuracy {
		t.Errorf("gate failed: laya accuracy %.2f is not greater than bm25 accuracy %.2f", layaAccuracy, bm25Accuracy)
	}
}

func loadEvalCases(t *testing.T) []evalCase {
	t.Helper()

	data, err := os.ReadFile("testdata/eval.json")
	if err != nil {
		t.Fatalf("read eval data: %v", err)
	}

	var cases []evalCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("decode eval data: %v", err)
	}
	return cases
}

func subsetResults(results []serp.Result, indices []int) []serp.Result {
	subset := make([]serp.Result, len(indices))
	for i, index := range indices {
		subset[i] = results[index]
	}
	return subset
}
