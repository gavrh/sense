package decide

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"unicode/utf8"

	"gavrh/sense/internal/extract"
	"gavrh/sense/internal/jev"
	"gavrh/sense/internal/serp"
)

const (
	defaultMaxOptions = 8
	maxCriteriaChars  = 120
	maxStateChars     = 4000 // cap one batched state; normal passages are far shorter

	answerKey = "answer"
)

type Options struct {
	Model         string  // forwarded to jev; empty means the client default
	MinConfidence float64 // if > 0, sent as min_confidence and used as the local abstention floor
	MaxOptions    int     // max candidates fed to one choice question; if <= 0 use 8
}

type Decider struct {
	client        *jev.Client
	model         string
	minConfidence float64
	maxOptions    int
}

// Selection identifies one chosen input by its index in the caller's slice.
// Index is -1 and Abstained is true when nothing cleared the confidence floor.
type Selection struct {
	Index      int
	Confidence float64
	Abstained  bool
}

func New(client *jev.Client, opts Options) *Decider {
	maxOptions := opts.MaxOptions
	if maxOptions <= 0 {
		maxOptions = defaultMaxOptions
	}

	return &Decider{
		client:        client,
		model:         opts.Model,
		minConfidence: opts.MinConfidence,
		maxOptions:    maxOptions,
	}
}

// RankResults picks the SERP result most likely to directly answer the query.
func (d *Decider) RankResults(ctx context.Context, query string, results []serp.Result) (Selection, error) {
	if len(results) == 0 {
		return abstained(0), nil
	}

	indices := candidateIndices(query, results, d.maxOptions)
	labels := make(map[string]int, len(indices))
	criteria := make(map[string]string, len(indices))

	// Shuffle so label position does not bias the model.
	for position, k := range rand.Perm(len(indices)) {
		label := fmt.Sprintf("c%d", position)
		labels[label] = indices[k]
		criteria[label] = summarize(results[indices[k]])
	}

	req := jev.Request{
		State: query,
		Questions: map[string]jev.Question{
			answerKey: {
				Type:         "choice",
				Instructions: "Pick the result most likely to directly answer the query. Reply with the label of the best result.",
				Criteria:     criteria,
			},
		},
		Model: d.model,
	}
	if d.minConfidence > 0 {
		req.MinConfidence = &d.minConfidence
	}

	resp, err := d.client.SystemOne(ctx, req)
	if err != nil {
		return Selection{}, fmt.Errorf("decide: rank results: %w", err)
	}

	answer, ok := resp.Answers[answerKey]
	if !ok || isNull(answer.Answer) {
		return abstained(0), nil
	}

	label, err := answer.AsString()
	if err != nil {
		return Selection{}, fmt.Errorf("decide: rank results: %w", err)
	}

	index, ok := labels[label]
	if !ok {
		return abstained(confidenceOf(answer)), nil
	}
	return d.finish(index, confidenceOf(answer)), nil
}

// SelectPassage picks the passage most likely to contain the answer.
func (d *Decider) SelectPassage(ctx context.Context, query string, passages []extract.Passage) (Selection, error) {
	states := make([]any, len(passages))
	for i, passage := range passages {
		states[i] = truncate(passage.Text, maxStateChars)
	}

	return d.selectBatched(ctx, query, states)
}

// SelectSentence picks the sentence most likely to contain the answer.
func (d *Decider) SelectSentence(ctx context.Context, query string, sentences []string) (Selection, error) {
	states := make([]any, len(sentences))
	for i, sentence := range sentences {
		states[i] = truncate(sentence, maxStateChars)
	}

	return d.selectBatched(ctx, query, states)
}

// selectBatched scores one noul question per state and keeps the highest-confidence true.
func (d *Decider) selectBatched(ctx context.Context, query string, states []any) (Selection, error) {
	if len(states) == 0 {
		return abstained(0), nil
	}

	req := jev.BatchRequest{
		States: states,
		Questions: map[string]jev.Question{
			answerKey: {
				Type:         "noul",
				Instructions: "Does this text contain a direct answer to: " + query + "?",
			},
		},
		Model: d.model,
	}
	if d.minConfidence > 0 {
		req.MinConfidence = &d.minConfidence
	}

	resp, err := d.client.SystemOneBatch(ctx, req)
	if err != nil {
		return Selection{}, fmt.Errorf("decide: select from states: %w", err)
	}

	var (
		bestIndex      = -1
		bestConfidence float64
		found          bool
	)
	for i, result := range resp.Results {
		if i >= len(states) {
			break
		}

		answer, ok := result.Answers[answerKey]
		if !ok || isNull(answer.Answer) {
			continue
		}
		if relevant, err := answer.AsBool(); err != nil || !relevant {
			continue
		}

		if confidence := confidenceOf(answer); !found || confidence > bestConfidence {
			bestIndex, bestConfidence, found = i, confidence, true
		}
	}

	if !found {
		return abstained(0), nil
	}
	return d.finish(bestIndex, bestConfidence), nil
}

func (d *Decider) finish(index int, confidence float64) Selection {
	if d.minConfidence > 0 && confidence < d.minConfidence {
		return abstained(confidence)
	}
	return Selection{Index: index, Confidence: confidence}
}

func abstained(confidence float64) Selection {
	return Selection{Index: -1, Confidence: confidence, Abstained: true}
}

func candidateIndices(query string, results []serp.Result, limit int) []int {
	if len(results) <= limit {
		indices := make([]int, len(results))
		for i := range indices {
			indices[i] = i
		}
		return indices
	}
	return Prefilter(query, results, limit)
}

func summarize(result serp.Result) string {
	return truncate(result.Title+" — "+result.Snippet, maxCriteriaChars)
}

func truncate(text string, max int) string {
	if max <= 0 || utf8.RuneCountInString(text) <= max {
		return text
	}
	return string([]rune(text)[:max])
}

func confidenceOf(answer jev.Answer) float64 {
	if answer.Confidence == nil {
		return 0
	}
	return *answer.Confidence
}

// isNull detects a JSON null answer; AsString turns null into "" without an error.
func isNull(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))
}
