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

// evalCase is one query, the URL substring a correct top-1 must contain, and an
// optional note on why that page is canonical.
//
// Ground truth rules:
//   - expect_url is the single canonical, authoritative page for the query
//     (official docs, a reference article, the canonical Stack Overflow answer),
//     not merely a page that mentions the terms.
//   - the query must have one unambiguous answer.
//   - the canonical URL must appear in live SearXNG results for the query, and
//     ideally in the prefiltered candidate set the decider sees (evalMaxOptions).
//   - the set deliberately mixes BM25-strong cases, where distinctive query
//     terms match the expected page title or snippet literally, so the keyword
//     baseline scores above zero.
type evalCase struct {
	Query     string `json:"query"`
	ExpectURL string `json:"expect_url"`
	Notes     string `json:"notes,omitempty"`
}

// TestEvalAgainstBM25 is the go/no-go gate: Laya must beat a plain BM25 baseline.
// It reports engine top-1, BM25 top-1, and Laya top-1 per case so a miss is
// diagnosable, and fails when the baseline is degenerate (BM25 scores zero).
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
	var engineHits, bm25Hits, layaHits, scored int

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

		engineHit := strings.Contains(results[0].URL, tc.ExpectURL)

		bm25Top := decide.Prefilter(tc.Query, results, 1)
		bm25Hit := strings.Contains(results[bm25Top[0]].URL, tc.ExpectURL)

		subset := subsetResults(results, decide.Prefilter(tc.Query, results, evalMaxOptions))
		if !subsetContains(subset, tc.ExpectURL) {
			t.Logf("%q: expected url absent from candidate set", tc.Query)
		}

		selection, err := decider.RankResults(ctx, tc.Query, subset)
		if err != nil {
			t.Errorf("%q: rank: %v", tc.Query, err)
			continue
		}
		layaHit := !selection.Abstained && strings.Contains(subset[selection.Index].URL, tc.ExpectURL)

		if engineHit {
			engineHits++
		}
		if bm25Hit {
			bm25Hits++
		}
		if layaHit {
			layaHits++
		}
		scored++

		layaURL := "abstained"
		if !selection.Abstained {
			layaURL = subset[selection.Index].URL
		}
		t.Logf("%-45q engine=%-5v bm25=%-5v laya=%-5v", tc.Query, engineHit, bm25Hit, layaHit)
		t.Logf("  engine: %s", results[0].URL)
		t.Logf("  bm25:   %s", results[bm25Top[0]].URL)
		t.Logf("  laya:   %s", layaURL)
	}

	if scored == 0 {
		t.Skip("no scored queries")
	}

	engineAccuracy := float64(engineHits) / float64(scored)
	bm25Accuracy := float64(bm25Hits) / float64(scored)
	layaAccuracy := float64(layaHits) / float64(scored)
	t.Logf("accuracy over %d queries: engine=%.2f bm25=%.2f laya=%.2f", scored, engineAccuracy, bm25Accuracy, layaAccuracy)

	if bm25Hits == 0 {
		t.Errorf("degenerate baseline: bm25 scored 0 of %d queries; the gate cannot pass vacuously", scored)
	}
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

func subsetContains(results []serp.Result, url string) bool {
	for _, result := range results {
		if strings.Contains(result.URL, url) {
			return true
		}
	}
	return false
}
