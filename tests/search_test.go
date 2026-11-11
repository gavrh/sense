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

type searchURLFetcher struct {
	pages map[string]*fetch.Page
	errs  map[string]error
	calls []string
}

func (f *searchURLFetcher) Get(ctx context.Context, rawURL string) (*fetch.Page, error) {
	f.calls = append(f.calls, rawURL)
	if err, ok := f.errs[rawURL]; ok {
		return nil, err
	}
	page, ok := f.pages[rawURL]
	if !ok {
		return nil, errors.New("unexpected url: " + rawURL)
	}
	return page, nil
}

type searchDecider struct {
	ranked      []decide.Selection
	rankErr     error
	passage     decide.Selection
	passageErr  error
	sentence    decide.Selection
	sentenceErr error

	rankResults []serp.Result
}

func (d *searchDecider) Ranked(ctx context.Context, query string, results []serp.Result) ([]decide.Selection, error) {
	d.rankResults = results
	return d.ranked, d.rankErr
}

func (d *searchDecider) SelectPassage(ctx context.Context, query string, passages []extract.Passage) (decide.Selection, error) {
	return d.passage, d.passageErr
}

func (d *searchDecider) SelectSentence(ctx context.Context, query string, sentences []string) (decide.Selection, error) {
	return d.sentence, d.sentenceErr
}

func newSearchPipeline(searcher *searchSearcher, fetcher search.Fetcher, decider *searchDecider, opts search.Options) *search.Pipeline {
	return search.New(searcher, fetcher, decider, opts)
}

func TestSearchPipelineHappyPath(t *testing.T) {
	searcher := &searchSearcher{results: []serp.Result{
		{Title: "First", URL: "https://one.example"},
		{Title: "Second", URL: "https://two.example"},
	}}
	fetcher := &searchFetcher{page: &fetch.Page{URL: "https://two.example/final", Body: []byte(searchArticleHTML)}}
	decider := &searchDecider{
		ranked:   []decide.Selection{{Index: 1, Confidence: 0.9}},
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
	decider := &searchDecider{}

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
	decider := &searchDecider{ranked: []decide.Selection{{Index: 0, Confidence: 0.9}}}

	_, err := newSearchPipeline(searcher, fetcher, decider, search.Options{}).Search(context.Background(), "query")
	if !errors.Is(err, fetchErr) {
		t.Fatalf("err = %v, want wrapped fetch error", err)
	}
	if !errors.Is(err, search.ErrSourcesUnavailable) {
		t.Fatalf("err = %v, want ErrSourcesUnavailable", err)
	}
}

func TestSearchPipelineFallsBackOnFetchError(t *testing.T) {
	searcher := &searchSearcher{results: []serp.Result{
		{Title: "First", URL: "https://one.example"},
		{Title: "Second", URL: "https://two.example"},
	}}
	fetchErr := errors.New("bad status: 400")
	fetcher := &searchURLFetcher{
		pages: map[string]*fetch.Page{
			"https://two.example": {URL: "https://two.example/final", Body: []byte(searchArticleHTML)},
		},
		errs: map[string]error{"https://one.example": fetchErr},
	}
	decider := &searchDecider{
		ranked:   []decide.Selection{{Index: 0, Confidence: 0.9}, {Index: 1, Confidence: 0.8}},
		passage:  decide.Selection{Index: 0, Confidence: 0.7},
		sentence: decide.Selection{Index: 1, Confidence: 0.6},
	}

	result, err := newSearchPipeline(searcher, fetcher, decider, search.Options{}).Search(context.Background(), "capital of france")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if result.Answer != "Beta answer sentence." {
		t.Errorf("answer = %q, want the second candidate's sentence", result.Answer)
	}
	if result.SourceTitle != "Second" || result.SourceURL != "https://two.example" {
		t.Errorf("source = %q %q, want the second candidate", result.SourceTitle, result.SourceURL)
	}
	if len(fetcher.calls) != 2 {
		t.Fatalf("fetch calls = %v, want two attempts", fetcher.calls)
	}
}

func TestSearchPipelineFallsBackOnNoPassages(t *testing.T) {
	searcher := &searchSearcher{results: []serp.Result{
		{Title: "First", URL: "https://one.example"},
		{Title: "Second", URL: "https://two.example"},
	}}
	fetcher := &searchURLFetcher{pages: map[string]*fetch.Page{
		"https://one.example": {URL: "https://one.example", Body: []byte("<html><body></body></html>")},
		"https://two.example": {URL: "https://two.example/final", Body: []byte(searchArticleHTML)},
	}}
	decider := &searchDecider{
		ranked:   []decide.Selection{{Index: 0, Confidence: 0.9}, {Index: 1, Confidence: 0.8}},
		passage:  decide.Selection{Index: 0, Confidence: 0.7},
		sentence: decide.Selection{Index: 1, Confidence: 0.6},
	}

	result, err := newSearchPipeline(searcher, fetcher, decider, search.Options{}).Search(context.Background(), "capital of france")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if result.SourceTitle != "Second" || result.SourceURL != "https://two.example" {
		t.Errorf("source = %q %q, want the second candidate", result.SourceTitle, result.SourceURL)
	}
	if len(fetcher.calls) != 2 {
		t.Fatalf("fetch calls = %v, want two attempts", fetcher.calls)
	}
}

func TestSearchPipelineAllSourcesUnavailable(t *testing.T) {
	searcher := &searchSearcher{results: []serp.Result{
		{Title: "First", URL: "https://one.example"},
		{Title: "Second", URL: "https://two.example"},
	}}
	fetcher := &searchURLFetcher{errs: map[string]error{
		"https://one.example": errors.New("status 400"),
		"https://two.example": errors.New("not html"),
	}}
	decider := &searchDecider{ranked: []decide.Selection{
		{Index: 0, Confidence: 0.9},
		{Index: 1, Confidence: 0.8},
	}}

	result, err := newSearchPipeline(searcher, fetcher, decider, search.Options{}).Search(context.Background(), "query")
	if !errors.Is(err, search.ErrSourcesUnavailable) {
		t.Fatalf("err = %v, want ErrSourcesUnavailable", err)
	}
	if result.Query != "query" {
		t.Errorf("query = %q, want query", result.Query)
	}
	if !strings.Contains(err.Error(), "status 400") || !strings.Contains(err.Error(), "not html") {
		t.Errorf("error = %v, want both candidate causes", err)
	}
}

func TestSearchPipelineMaxAttempts(t *testing.T) {
	searcher := &searchSearcher{results: []serp.Result{
		{Title: "One", URL: "https://one.example"},
		{Title: "Two", URL: "https://two.example"},
		{Title: "Three", URL: "https://three.example"},
	}}
	fetcher := &searchURLFetcher{errs: map[string]error{
		"https://one.example":   errors.New("status 400"),
		"https://two.example":   errors.New("status 403"),
		"https://three.example": errors.New("not html"),
	}}
	decider := &searchDecider{ranked: []decide.Selection{
		{Index: 0, Confidence: 0.9},
		{Index: 1, Confidence: 0.8},
		{Index: 2, Confidence: 0.7},
	}}

	_, err := newSearchPipeline(searcher, fetcher, decider, search.Options{MaxAttempts: 2}).Search(context.Background(), "query")
	if !errors.Is(err, search.ErrSourcesUnavailable) {
		t.Fatalf("err = %v, want ErrSourcesUnavailable", err)
	}
	if len(fetcher.calls) != 2 {
		t.Fatalf("fetch calls = %v, want two attempts", fetcher.calls)
	}
}

func TestSearchPipelineSelectionErrorPropagates(t *testing.T) {
	searcher := &searchSearcher{results: []serp.Result{{Title: "Only", URL: "https://example.com"}}}
	fetcher := &searchFetcher{page: &fetch.Page{URL: "https://example.com/final", Body: []byte(searchArticleHTML)}}
	selectionErr := errors.New("jev unavailable")
	decider := &searchDecider{
		ranked:     []decide.Selection{{Index: 0, Confidence: 0.9}},
		passageErr: selectionErr,
	}

	_, err := newSearchPipeline(searcher, fetcher, decider, search.Options{}).Search(context.Background(), "query")
	if !errors.Is(err, selectionErr) {
		t.Fatalf("err = %v, want the selection error", err)
	}
	if errors.Is(err, search.ErrSourcesUnavailable) {
		t.Fatalf("err = %v, want a non-source failure", err)
	}
}

func TestSearchPipelineNoPassages(t *testing.T) {
	searcher := &searchSearcher{results: []serp.Result{{Title: "Only", URL: "https://example.com"}}}
	fetcher := &searchFetcher{page: &fetch.Page{URL: "https://example.com", Body: []byte("<html><body></body></html>")}}
	decider := &searchDecider{ranked: []decide.Selection{{Index: 0, Confidence: 0.9}}}

	if _, err := newSearchPipeline(searcher, fetcher, decider, search.Options{}).Search(context.Background(), "query"); !errors.Is(err, search.ErrSourcesUnavailable) {
		t.Fatalf("err = %v, want ErrSourcesUnavailable", err)
	}
}

func TestSearchPipelineFallbacks(t *testing.T) {
	t.Run("sentence abstains keeps passage", func(t *testing.T) {
		searcher := &searchSearcher{results: []serp.Result{{Title: "Only", URL: "https://example.com"}}}
		fetcher := &searchFetcher{page: &fetch.Page{URL: "https://example.com", Body: []byte(searchArticleHTML)}}
		decider := &searchDecider{
			ranked:   []decide.Selection{{Index: 0, Confidence: 0.9}},
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
			ranked:   []decide.Selection{{Index: 0, Confidence: 0.9}},
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

	decider := &searchDecider{ranked: []decide.Selection{{Index: 0, Confidence: 0.9}}}
	if _, err := newSearchPipeline(searcher, &searchFetcher{}, decider, search.Options{}).Search(ctx, "query"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestSearchPipelineCapsResultsTopN(t *testing.T) {
	searcher := &searchSearcher{results: []serp.Result{
		{Title: "One", URL: "https://one.example"},
		{Title: "Two", URL: "https://two.example"},
		{Title: "Three", URL: "https://three.example"},
	}}
	decider := &searchDecider{}

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
