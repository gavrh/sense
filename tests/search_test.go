package tests

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gavrh/sense/internal/decide"
	"gavrh/sense/internal/extract"
	"gavrh/sense/internal/fetch"
	"gavrh/sense/internal/search"
	"gavrh/sense/internal/serp"
)

// searchArticleHTML is small, readable input for the pipeline's extractor.
const searchArticleHTML = `<html><body><article>
	<p>Alpha sentence. Beta answer sentence.</p>
</article></body></html>`

type searchSearcher struct {
	results []serp.Result
	err     error
}

func (s *searchSearcher) Search(ctx context.Context, query string) ([]serp.Result, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.results, nil
}

type searchFetcher struct {
	page *fetch.Page
	err  error
	url  string
}

func (f *searchFetcher) Get(ctx context.Context, rawURL string) (*fetch.Page, error) {
	f.url = rawURL
	if f.err != nil {
		return nil, f.err
	}
	return f.page, nil
}

type searchDecider struct {
	rank        decide.Selection
	rankErr     error
	passage     decide.Selection
	passageErr  error
	sentence    decide.Selection
	sentenceErr error

	rankResults []serp.Result
}

func (d *searchDecider) RankResults(ctx context.Context, query string, results []serp.Result) (decide.Selection, error) {
	d.rankResults = results
	return d.rank, d.rankErr
}

func (d *searchDecider) SelectPassage(ctx context.Context, query string, passages []extract.Passage) (decide.Selection, error) {
	return d.passage, d.passageErr
}

func (d *searchDecider) SelectSentence(ctx context.Context, query string, sentences []string) (decide.Selection, error) {
	return d.sentence, d.sentenceErr
}

func newSearchPipeline(searcher *searchSearcher, fetcher *searchFetcher, decider *searchDecider, opts search.Options) *search.Pipeline {
	return search.New(searcher, fetcher, decider, opts)
}

func TestSearchPipelineHappyPath(t *testing.T) {
	searcher := &searchSearcher{results: []serp.Result{
		{Title: "First", URL: "https://one.example"},
		{Title: "Second", URL: "https://two.example"},
	}}
	fetcher := &searchFetcher{page: &fetch.Page{URL: "https://two.example/final", Body: []byte(searchArticleHTML)}}
	decider := &searchDecider{
		rank:     decide.Selection{Index: 1, Confidence: 0.9},
		passage:  decide.Selection{Index: 0, Confidence: 0.8},
		sentence: decide.Selection{Index: 1, Confidence: 0.95},
	}

	result, err := newSearchPipeline(searcher, fetcher, decider, search.Options{}).Search(context.Background(), "capital of france")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if result.Answer != "Beta answer sentence." {
		t.Errorf("answer = %q, want the chosen sentence", result.Answer)
	}
	if result.SourceTitle != "Second" {
		t.Errorf("title = %q, want Second", result.SourceTitle)
	}
	if result.SourceURL != "https://two.example" {
		t.Errorf("url = %q, want the candidate URL", result.SourceURL)
	}
	if result.Confidence != 0.95 {
		t.Errorf("confidence = %v, want 0.95", result.Confidence)
	}
	if fetcher.url != "https://two.example" {
		t.Errorf("fetched %q, want the candidate URL", fetcher.url)
	}
}

func TestSearchPipelineNoResults(t *testing.T) {
	pipeline := newSearchPipeline(&searchSearcher{}, &searchFetcher{}, &searchDecider{}, search.Options{})

	if _, err := pipeline.Search(context.Background(), "anything"); !errors.Is(err, search.ErrNoResults) {
		t.Fatalf("err = %v, want ErrNoResults", err)
	}
}

func TestSearchPipelineRankingAbstains(t *testing.T) {
	searcher := &searchSearcher{results: []serp.Result{{Title: "Only", URL: "https://example.com"}}}
	decider := &searchDecider{rank: decide.Selection{Index: -1, Abstained: true}}

	result, err := newSearchPipeline(searcher, &searchFetcher{}, decider, search.Options{}).Search(context.Background(), "query")
	if !errors.Is(err, search.ErrNoAnswer) {
		t.Fatalf("err = %v, want ErrNoAnswer", err)
	}
	if result.Query != "query" {
		t.Errorf("query = %q, want query", result.Query)
	}
}

func TestSearchPipelineFetchError(t *testing.T) {
	fetchErr := errors.New("connection refused")
	searcher := &searchSearcher{results: []serp.Result{{Title: "Only", URL: "https://example.com"}}}
	fetcher := &searchFetcher{err: fetchErr}
	decider := &searchDecider{rank: decide.Selection{Index: 0, Confidence: 0.9}}

	_, err := newSearchPipeline(searcher, fetcher, decider, search.Options{}).Search(context.Background(), "query")
	if !errors.Is(err, fetchErr) {
		t.Fatalf("err = %v, want wrapped fetch error", err)
	}
}

func TestSearchPipelineNoPassages(t *testing.T) {
	searcher := &searchSearcher{results: []serp.Result{{Title: "Only", URL: "https://example.com"}}}
	fetcher := &searchFetcher{page: &fetch.Page{URL: "https://example.com", Body: []byte("<html><body></body></html>")}}
	decider := &searchDecider{rank: decide.Selection{Index: 0, Confidence: 0.9}}

	if _, err := newSearchPipeline(searcher, fetcher, decider, search.Options{}).Search(context.Background(), "query"); !errors.Is(err, search.ErrNoAnswer) {
		t.Fatalf("err = %v, want ErrNoAnswer", err)
	}
}

func TestSearchPipelineFallbacks(t *testing.T) {
	t.Run("sentence abstains keeps passage", func(t *testing.T) {
		searcher := &searchSearcher{results: []serp.Result{{Title: "Only", URL: "https://example.com"}}}
		fetcher := &searchFetcher{page: &fetch.Page{URL: "https://example.com", Body: []byte(searchArticleHTML)}}
		decider := &searchDecider{
			rank:     decide.Selection{Index: 0, Confidence: 0.9},
			passage:  decide.Selection{Index: 0, Confidence: 0.6},
			sentence: decide.Selection{Index: -1, Abstained: true},
		}

		result, err := newSearchPipeline(searcher, fetcher, decider, search.Options{}).Search(context.Background(), "capital of france")
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if !containsAll(result.Answer, "Alpha sentence.", "Beta answer sentence.") {
			t.Errorf("answer = %q, want the full passage", result.Answer)
		}
		if result.Confidence != 0.6 {
			t.Errorf("confidence = %v, want the passage confidence", result.Confidence)
		}
	})

	t.Run("passage abstains uses sentence", func(t *testing.T) {
		searcher := &searchSearcher{results: []serp.Result{{Title: "Only", URL: "https://example.com"}}}
		fetcher := &searchFetcher{page: &fetch.Page{URL: "https://example.com", Body: []byte(searchArticleHTML)}}
		decider := &searchDecider{
			rank:     decide.Selection{Index: 0, Confidence: 0.9},
			passage:  decide.Selection{Index: -1, Abstained: true},
			sentence: decide.Selection{Index: 1, Confidence: 0.7},
		}

		result, err := newSearchPipeline(searcher, fetcher, decider, search.Options{}).Search(context.Background(), "capital of france")
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if result.Answer != "Beta answer sentence." {
			t.Errorf("answer = %q, want the fallback sentence", result.Answer)
		}
		if result.Confidence != 0.7 {
			t.Errorf("confidence = %v, want 0.7", result.Confidence)
		}
	})
}

func TestSearchPipelineRejectsEmptyQuery(t *testing.T) {
	pipeline := newSearchPipeline(&searchSearcher{}, &searchFetcher{}, &searchDecider{}, search.Options{})

	if _, err := pipeline.Search(context.Background(), "   "); err == nil {
		t.Fatal("expected an error for an empty query")
	}
}

func TestSearchPipelineCancelledContext(t *testing.T) {
	searcher := &searchSearcher{results: []serp.Result{{Title: "Only", URL: "https://example.com"}}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := newSearchPipeline(searcher, &searchFetcher{}, &searchDecider{}, search.Options{}).Search(ctx, "query"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestSearchPipelineCapsResultsTopN(t *testing.T) {
	searcher := &searchSearcher{results: []serp.Result{
		{Title: "One", URL: "https://one.example"},
		{Title: "Two", URL: "https://two.example"},
		{Title: "Three", URL: "https://three.example"},
	}}
	decider := &searchDecider{rank: decide.Selection{Index: -1, Abstained: true}}

	_, err := newSearchPipeline(searcher, &searchFetcher{}, decider, search.Options{TopN: 2}).Search(context.Background(), "query")
	if !errors.Is(err, search.ErrNoAnswer) {
		t.Fatalf("err = %v, want ErrNoAnswer", err)
	}
	if len(decider.rankResults) != 2 {
		t.Errorf("ranked %d results, want 2", len(decider.rankResults))
	}
}

func containsAll(text string, substrings ...string) bool {
	for _, substring := range substrings {
		if !strings.Contains(text, substring) {
			return false
		}
	}
	return true
}
