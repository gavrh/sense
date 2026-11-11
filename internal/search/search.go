package search

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"gavrh/sense/internal/decide"
	"gavrh/sense/internal/extract"
	"gavrh/sense/internal/fetch"
	"gavrh/sense/internal/serp"
)

const (
	defaultTopN        = 10
	defaultMaxAttempts = 3
)

var (
	ErrNoResults          = errors.New("search: no results")
	ErrNoAnswer           = errors.New("search: no answer")
	ErrSourcesUnavailable = errors.New("search: no readable source")
)

type Result struct {
	Query       string
	Answer      string
	SourceURL   string
	SourceTitle string
	Confidence  float64
}

type Options struct {
	TopN        int
	MaxAttempts int
	Extract     extract.Options
}

type Searcher interface {
	Search(ctx context.Context, query string) ([]serp.Result, error)
}

type Fetcher interface {
	Get(ctx context.Context, url string) (*fetch.Page, error)
}

type Decider interface {
	Ranked(ctx context.Context, query string, results []serp.Result) ([]decide.Selection, error)
	SelectPassage(ctx context.Context, query string, passages []extract.Passage) (decide.Selection, error)
	SelectSentence(ctx context.Context, query string, sentences []string) (decide.Selection, error)
}

type Pipeline struct {
	searcher    Searcher
	fetcher     Fetcher
	decider     Decider
	topN        int
	maxAttempts int
	extract     extract.Options
	extractFn   func(io.Reader, string, extract.Options) ([]extract.Passage, error)
}

func New(s Searcher, f Fetcher, d Decider, opts Options) *Pipeline {
	topN := opts.TopN
	if topN <= 0 {
		topN = defaultTopN
	}
	maxAttempts := opts.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = defaultMaxAttempts
	}

	return &Pipeline{
		searcher:    s,
		fetcher:     f,
		decider:     d,
		topN:        topN,
		maxAttempts: maxAttempts,
		extract:     opts.Extract,
		extractFn:   extract.Extract,
	}
}

func (p *Pipeline) Search(ctx context.Context, query string) (Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return Result{}, errors.New("search: query is empty")
	}
	if err := ctx.Err(); err != nil {
		return Result{Query: query}, err
	}

	results, err := p.searcher.Search(ctx, query)
	if err != nil {
		return Result{Query: query}, fmt.Errorf("search: find results: %w", err)
	}
	if len(results) == 0 {
		return Result{Query: query}, ErrNoResults
	}

	results = capResults(results, p.topN)

	ranked, err := p.decider.Ranked(ctx, query, results)
	if err != nil {
		return Result{Query: query}, fmt.Errorf("search: rank results: %w", err)
	}
	if len(ranked) == 0 {
		return Result{Query: query}, ErrNoAnswer
	}

	result := Result{Query: query}
	var failures []error

	attempts := min(p.maxAttempts, len(ranked))
	for _, ranking := range ranked[:attempts] {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if !validSelection(ranking, len(results)) {
			failures = append(failures, fmt.Errorf("search: invalid candidate index %d", ranking.Index))
			continue
		}

		candidate := results[ranking.Index]

		page, err := p.fetcher.Get(ctx, candidate.URL)
		if err != nil {
			failures = append(failures, fmt.Errorf("search: fetch %q: %w", candidate.URL, err))
			continue
		}

		passages, err := p.extractFn(bytes.NewReader(page.Body), page.URL, p.extract)
		if err != nil {
			failures = append(failures, fmt.Errorf("search: extract %q: %w", candidate.URL, err))
			continue
		}
		if len(passages) == 0 {
			failures = append(failures, fmt.Errorf("search: extract %q: no passages", candidate.URL))
			continue
		}

		result.SourceURL = candidate.URL
		result.SourceTitle = candidate.Title

		passageSelection, err := p.decider.SelectPassage(ctx, query, passages)
		if err != nil {
			return result, fmt.Errorf("search: select passage: %w", err)
		}

		passage := passages[0]
		if validSelection(passageSelection, len(passages)) {
			passage = passages[passageSelection.Index]
			result.Confidence = passageSelection.Confidence
		}
		result.Answer = passage.Text

		sentences := extract.Sentences(passage.Text)
		sentenceSelection, err := p.decider.SelectSentence(ctx, query, sentences)
		if err != nil {
			return result, fmt.Errorf("search: select sentence: %w", err)
		}
		if validSelection(sentenceSelection, len(sentences)) {
			result.Answer = sentences[sentenceSelection.Index]
			result.Confidence = sentenceSelection.Confidence
		}

		return result, nil
	}

	if err := ctx.Err(); err != nil {
		return result, err
	}
	return result, errors.Join(ErrSourcesUnavailable, errors.Join(failures...))
}

func capResults(results []serp.Result, topN int) []serp.Result {
	if len(results) > topN {
		return results[:topN]
	}
	return results
}

func validSelection(selection decide.Selection, length int) bool {
	return !selection.Abstained && selection.Index >= 0 && selection.Index < length
}
