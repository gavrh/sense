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

const defaultTopN = 10

var (
	ErrNoResults = errors.New("search: no results")
	ErrNoAnswer  = errors.New("search: no answer")
)

type Result struct {
	Query       string
	Answer      string
	SourceURL   string
	SourceTitle string
	Confidence  float64
}

type Options struct {
	TopN    int
	Extract extract.Options
}

type Searcher interface {
	Search(ctx context.Context, query string) ([]serp.Result, error)
}

type Fetcher interface {
	Get(ctx context.Context, url string) (*fetch.Page, error)
}

type Decider interface {
	RankResults(ctx context.Context, query string, results []serp.Result) (decide.Selection, error)
	SelectPassage(ctx context.Context, query string, passages []extract.Passage) (decide.Selection, error)
	SelectSentence(ctx context.Context, query string, sentences []string) (decide.Selection, error)
}

type Pipeline struct {
	searcher  Searcher
	fetcher   Fetcher
	decider   Decider
	topN      int
	extract   extract.Options
	extractFn func(io.Reader, string, extract.Options) ([]extract.Passage, error)
}

func New(s Searcher, f Fetcher, d Decider, opts Options) *Pipeline {
	topN := opts.TopN
	if topN <= 0 {
		topN = defaultTopN
	}

	return &Pipeline{
		searcher:  s,
		fetcher:   f,
		decider:   d,
		topN:      topN,
		extract:   opts.Extract,
		extractFn: extract.Extract,
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

	ranking, err := p.decider.RankResults(ctx, query, results)
	if err != nil {
		return Result{Query: query}, fmt.Errorf("search: rank results: %w", err)
	}
	if !validSelection(ranking, len(results)) {
		return Result{Query: query}, ErrNoAnswer
	}

	candidate := results[ranking.Index]

	page, err := p.fetcher.Get(ctx, candidate.URL)
	if err != nil {
		return Result{Query: query}, fmt.Errorf("search: fetch %q: %w", candidate.URL, err)
	}

	passages, err := p.extractFn(bytes.NewReader(page.Body), page.URL, p.extract)
	if err != nil {
		return Result{Query: query}, fmt.Errorf("search: extract: %w", err)
	}
	if len(passages) == 0 {
		return Result{Query: query}, ErrNoAnswer
	}

	result := Result{
		Query:       query,
		SourceURL:   candidate.URL,
		SourceTitle: candidate.Title,
	}

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

func capResults(results []serp.Result, topN int) []serp.Result {
	if len(results) > topN {
		return results[:topN]
	}
	return results
}

func validSelection(selection decide.Selection, length int) bool {
	return !selection.Abstained && selection.Index >= 0 && selection.Index < length
}
