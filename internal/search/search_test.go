package search

import (
	"context"
	"errors"
	"io"
	"testing"

	"gavrh/sense/internal/decide"
	"gavrh/sense/internal/extract"
	"gavrh/sense/internal/fetch"
	"gavrh/sense/internal/serp"
)

type fakeSearcher struct {
	results []serp.Result
	err     error
	calls   int
}

func (f *fakeSearcher) Search(ctx context.Context, query string) ([]serp.Result, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.results, nil
}

type fakeFetcher struct {
	page  *fetch.Page
	err   error
	calls int
	url   string
}

func (f *fakeFetcher) Get(ctx context.Context, rawURL string) (*fetch.Page, error) {
	f.calls++
	f.url = rawURL
	if f.err != nil {
		return nil, f.err
	}
	return f.page, nil
}

type fakeDecider struct {
	rank        decide.Selection
	rankErr     error
	passage     decide.Selection
	passageErr  error
	sentence    decide.Selection
	sentenceErr error

	rankCalls     int
	passageCalls  int
	sentenceCalls int

	rankResults []serp.Result
	passages    []extract.Passage
	sentences   []string
}

func (f *fakeDecider) RankResults(ctx context.Context, query string, results []serp.Result) (decide.Selection, error) {
	f.rankCalls++
	f.rankResults = results
	return f.rank, f.rankErr
}

func (f *fakeDecider) SelectPassage(ctx context.Context, query string, passages []extract.Passage) (decide.Selection, error) {
	f.passageCalls++
	f.passages = passages
	return f.passage, f.passageErr
}

func (f *fakeDecider) SelectSentence(ctx context.Context, query string, sentences []string) (decide.Selection, error) {
	f.sentenceCalls++
	f.sentences = sentences
	return f.sentence, f.sentenceErr
}

func fakeExtractor(passages []extract.Passage, err error) func(io.Reader, string, extract.Options) ([]extract.Passage, error) {
	return func(io.Reader, string, extract.Options) ([]extract.Passage, error) {
		return passages, err
	}
}

func TestSearchHappyPath(t *testing.T) {
	searcher := &fakeSearcher{results: []serp.Result{
		{Title: "First", URL: "https://one.example"},
		{Title: "Second", URL: "https://two.example"},
	}}
	fetcher := &fakeFetcher{page: &fetch.Page{
		URL:  "https://two.example/final",
		Body: []byte("<html>page</html>"),
	}}
	decider := &fakeDecider{
		rank:     decide.Selection{Index: 1, Confidence: 0.9},
		passage:  decide.Selection{Index: 0, Confidence: 0.8},
		sentence: decide.Selection{Index: 0, Confidence: 0.95},
	}

	pipeline := New(searcher, fetcher, decider, Options{})
	pipeline.extractFn = fakeExtractor([]extract.Passage{
		{Index: 0, Text: "Paris is the capital of France. It is large."},
	}, nil)

	result, err := pipeline.Search(context.Background(), "capital of france")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if result.Answer != "Paris is the capital of France." {
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

func TestSearchNoResults(t *testing.T) {
	searcher := &fakeSearcher{}
	fetcher := &fakeFetcher{}
	decider := &fakeDecider{}

	pipeline := New(searcher, fetcher, decider, Options{})
	_, err := pipeline.Search(context.Background(), "anything")

	if !errors.Is(err, ErrNoResults) {
		t.Fatalf("err = %v, want ErrNoResults", err)
	}
	if fetcher.calls != 0 || decider.rankCalls != 0 {
		t.Errorf("dependencies called: fetch=%d rank=%d", fetcher.calls, decider.rankCalls)
	}
}

func TestSearchRankingAbstains(t *testing.T) {
	searcher := &fakeSearcher{results: []serp.Result{{Title: "Only", URL: "https://example.com"}}}
	fetcher := &fakeFetcher{}
	decider := &fakeDecider{rank: decide.Selection{Index: -1, Abstained: true}}

	pipeline := New(searcher, fetcher, decider, Options{})
	result, err := pipeline.Search(context.Background(), "query")

	if !errors.Is(err, ErrNoAnswer) {
		t.Fatalf("err = %v, want ErrNoAnswer", err)
	}
	if result.Query != "query" {
		t.Errorf("query = %q, want query", result.Query)
	}
	if fetcher.calls != 0 {
		t.Errorf("fetcher called %d times, want 0", fetcher.calls)
	}
}

func TestSearchFetchError(t *testing.T) {
	fetchErr := errors.New("connection refused")
	searcher := &fakeSearcher{results: []serp.Result{{Title: "Only", URL: "https://example.com"}}}
	fetcher := &fakeFetcher{err: fetchErr}
	decider := &fakeDecider{rank: decide.Selection{Index: 0, Confidence: 0.9}}

	pipeline := New(searcher, fetcher, decider, Options{})
	_, err := pipeline.Search(context.Background(), "query")

	if !errors.Is(err, fetchErr) {
		t.Fatalf("err = %v, want wrapped fetch error", err)
	}
	if errors.Is(err, ErrNoAnswer) {
		t.Errorf("fetch error should not match ErrNoAnswer")
	}
}

func TestSearchNoPassages(t *testing.T) {
	searcher := &fakeSearcher{results: []serp.Result{{Title: "Only", URL: "https://example.com"}}}
	fetcher := &fakeFetcher{page: &fetch.Page{URL: "https://example.com", Body: []byte("html")}}
	decider := &fakeDecider{rank: decide.Selection{Index: 0, Confidence: 0.9}}

	pipeline := New(searcher, fetcher, decider, Options{})
	pipeline.extractFn = fakeExtractor(nil, nil)

	_, err := pipeline.Search(context.Background(), "query")
	if !errors.Is(err, ErrNoAnswer) {
		t.Fatalf("err = %v, want ErrNoAnswer", err)
	}
}

func TestSearchPassageAbstainsFallsBackToSentences(t *testing.T) {
	searcher := &fakeSearcher{results: []serp.Result{{Title: "Only", URL: "https://example.com"}}}
	fetcher := &fakeFetcher{page: &fetch.Page{URL: "https://example.com", Body: []byte("html")}}
	decider := &fakeDecider{
		rank:     decide.Selection{Index: 0, Confidence: 0.9},
		passage:  decide.Selection{Index: -1, Abstained: true},
		sentence: decide.Selection{Index: 1, Confidence: 0.7},
	}

	pipeline := New(searcher, fetcher, decider, Options{})
	pipeline.extractFn = fakeExtractor([]extract.Passage{
		{Index: 0, Text: "Alpha sentence. Beta answer sentence."},
		{Index: 1, Text: "Unrelated passage."},
	}, nil)

	result, err := pipeline.Search(context.Background(), "query")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if result.Answer != "Beta answer sentence." {
		t.Errorf("answer = %q, want the sentence from the fallback passage", result.Answer)
	}
	if result.Confidence != 0.7 {
		t.Errorf("confidence = %v, want 0.7", result.Confidence)
	}
	if len(decider.sentences) == 0 {
		t.Error("sentence selection was not exercised")
	}
}

func TestSearchSentenceAbstainsKeepsPassage(t *testing.T) {
	const passageText = "Paris is the capital of France. It is large."

	searcher := &fakeSearcher{results: []serp.Result{{Title: "Only", URL: "https://example.com"}}}
	fetcher := &fakeFetcher{page: &fetch.Page{URL: "https://example.com", Body: []byte("html")}}
	decider := &fakeDecider{
		rank:     decide.Selection{Index: 0, Confidence: 0.9},
		passage:  decide.Selection{Index: 0, Confidence: 0.6},
		sentence: decide.Selection{Index: -1, Abstained: true},
	}

	pipeline := New(searcher, fetcher, decider, Options{})
	pipeline.extractFn = fakeExtractor([]extract.Passage{{Index: 0, Text: passageText}}, nil)

	result, err := pipeline.Search(context.Background(), "capital of france")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if result.Answer != passageText {
		t.Errorf("answer = %q, want the passage text", result.Answer)
	}
	if result.Confidence != 0.6 {
		t.Errorf("confidence = %v, want the passage confidence", result.Confidence)
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	searcher := &fakeSearcher{}
	fetcher := &fakeFetcher{}
	decider := &fakeDecider{}

	pipeline := New(searcher, fetcher, decider, Options{})
	_, err := pipeline.Search(context.Background(), "   ")

	if err == nil {
		t.Fatal("expected an error for an empty query")
	}
	if searcher.calls != 0 || fetcher.calls != 0 || decider.rankCalls != 0 {
		t.Errorf("dependencies called: search=%d fetch=%d rank=%d", searcher.calls, fetcher.calls, decider.rankCalls)
	}
}

func TestSearchCancelledContext(t *testing.T) {
	searcher := &fakeSearcher{results: []serp.Result{{Title: "Only", URL: "https://example.com"}}}
	fetcher := &fakeFetcher{}
	decider := &fakeDecider{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	pipeline := New(searcher, fetcher, decider, Options{})
	_, err := pipeline.Search(ctx, "query")

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if searcher.calls != 0 || fetcher.calls != 0 || decider.rankCalls != 0 {
		t.Errorf("dependencies called: search=%d fetch=%d rank=%d", searcher.calls, fetcher.calls, decider.rankCalls)
	}
}

func TestSearchCapsResultsAtTopN(t *testing.T) {
	searcher := &fakeSearcher{results: []serp.Result{
		{Title: "One", URL: "https://one.example"},
		{Title: "Two", URL: "https://two.example"},
		{Title: "Three", URL: "https://three.example"},
	}}
	fetcher := &fakeFetcher{}
	decider := &fakeDecider{rank: decide.Selection{Index: -1, Abstained: true}}

	pipeline := New(searcher, fetcher, decider, Options{TopN: 2})
	_, err := pipeline.Search(context.Background(), "query")

	if !errors.Is(err, ErrNoAnswer) {
		t.Fatalf("err = %v, want ErrNoAnswer", err)
	}
	if len(decider.rankResults) != 2 {
		t.Errorf("ranked %d results, want 2", len(decider.rankResults))
	}
}
