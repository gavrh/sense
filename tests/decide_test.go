package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gavrh/sense/internal/decide"
	"gavrh/sense/internal/extract"
	"gavrh/sense/internal/jev"
	"gavrh/sense/internal/serp"
)

const decideAnswerKey = "answer"

func newDecideDecider(t *testing.T, server *httptest.Server, opts decide.Options) *decide.Decider {
	t.Helper()

	return decide.New(jev.New(server.URL, "", "english", 5*time.Second), opts)
}

func decideLabelFor(req jev.Request, needle string) (string, bool) {
	criteria, ok := req.Questions[decideAnswerKey].Criteria.(map[string]any)
	if !ok {
		return "", false
	}
	for label, description := range criteria {
		if strings.Contains(description.(string), needle) {
			return label, true
		}
	}
	return "", false
}

func decideCriteriaContains(req jev.Request, needle string) bool {
	criteria, ok := req.Questions[decideAnswerKey].Criteria.(map[string]any)
	if !ok {
		return false
	}
	for _, description := range criteria {
		if strings.Contains(description.(string), needle) {
			return true
		}
	}
	return false
}

func TestDecideRankResults(t *testing.T) {
	const query = "what is the capital of france"

	results := []serp.Result{
		{Title: "Britannica", URL: "https://britannica.example/france", Snippet: "France overview."},
		{Title: "Paris Guide", URL: "https://example.com/paris", Snippet: "Everything about Paris."},
		{Title: "Weather", URL: "https://example.com/weather", Snippet: "Today's forecast."},
	}

	var got jev.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/systemone" {
			t.Errorf("path = %q, want /v1/systemone", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}

		label, ok := decideLabelFor(got, "Paris Guide")
		if !ok {
			t.Errorf("no criterion for Paris Guide")
		}
		fmt.Fprintf(w, `{"answers":{"answer":{"answer":%q,"confidence":0.9}}}`, label)
	}))
	t.Cleanup(server.Close)

	selection, err := newDecideDecider(t, server, decide.Options{}).RankResults(context.Background(), query, results)
	if err != nil {
		t.Fatalf("RankResults: %v", err)
	}

	if got.State != query {
		t.Errorf("state = %v, want %q", got.State, query)
	}
	if len(got.Questions) != 1 {
		t.Errorf("questions = %d, want 1", len(got.Questions))
	}
	if question := got.Questions[decideAnswerKey]; question.Type != "choice" {
		t.Errorf("type = %q, want choice", question.Type)
	}
	for _, title := range []string{"Britannica", "Paris Guide", "Weather"} {
		if !decideCriteriaContains(got, title) {
			t.Errorf("criteria missing %q", title)
		}
	}
	if selection.Index != 1 || selection.Abstained {
		t.Errorf("selection = %+v, want index 1", selection)
	}
}

func TestDecideSelectPassage(t *testing.T) {
	passages := []extract.Passage{
		{Index: 0, Text: "The sky is blue."},
		{Index: 1, Text: "Paris is the capital of France."},
		{Index: 2, Text: "Water boils at 100C."},
	}

	var got jev.BatchRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/systemone/batch" {
			t.Errorf("path = %q, want /v1/systemone/batch", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		io.WriteString(w, `{"results":[
			{"answers":{"answer":{"answer":true,"confidence":0.4}}},
			{"answers":{"answer":{"answer":true,"confidence":0.9}}},
			{"answers":{"answer":{"answer":false,"confidence":0.99}}}
		]}`)
	}))
	t.Cleanup(server.Close)

	selection, err := newDecideDecider(t, server, decide.Options{}).SelectPassage(context.Background(), "capital of france", passages)
	if err != nil {
		t.Fatalf("SelectPassage: %v", err)
	}

	if len(got.States) != len(passages) {
		t.Fatalf("states = %d, want %d", len(got.States), len(passages))
	}
	if got.States[1] != passages[1].Text {
		t.Errorf("state[1] = %v, want %q", got.States[1], passages[1].Text)
	}
	question := got.Questions[decideAnswerKey]
	if question.Type != "noul" {
		t.Errorf("type = %q, want noul", question.Type)
	}
	if !strings.Contains(question.Instructions, "capital of france") {
		t.Errorf("instructions = %q, want the query", question.Instructions)
	}
	if selection.Index != 1 || selection.Abstained {
		t.Errorf("selection = %+v, want index 1", selection)
	}
}

func TestDecideSelectSentence(t *testing.T) {
	sentences := []string{"Alpha.", "Beta is the answer.", "Gamma."}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"results":[
			{"answers":{"answer":{"answer":false,"confidence":0.99}}},
			{"answers":{"answer":{"answer":true,"confidence":0.7}}},
			{"answers":{"answer":{"answer":true,"confidence":0.3}}}
		]}`)
	}))
	t.Cleanup(server.Close)

	selection, err := newDecideDecider(t, server, decide.Options{}).SelectSentence(context.Background(), "what is beta", sentences)
	if err != nil {
		t.Fatalf("SelectSentence: %v", err)
	}
	if selection.Index != 1 || selection.Abstained {
		t.Errorf("selection = %+v, want index 1", selection)
	}
}

func TestDecideAbstains(t *testing.T) {
	results := []serp.Result{{Title: "Only", URL: "https://example.com", Snippet: "snippet"}}

	cases := []struct {
		name    string
		body    string
		options decide.Options
	}{
		{"null answer", `{"answers":{"answer":{"answer":null,"confidence":0.9}}}`, decide.Options{}},
		{"below floor", `{"answers":{"answer":{"answer":"c0","confidence":0.2}}}`, decide.Options{MinConfidence: 0.5}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.WriteString(w, tc.body)
			}))
			t.Cleanup(server.Close)

			selection, err := newDecideDecider(t, server, tc.options).RankResults(context.Background(), "query", results)
			if err != nil {
				t.Fatalf("RankResults: %v", err)
			}
			if !selection.Abstained || selection.Index != -1 {
				t.Errorf("selection = %+v, want abstained index -1", selection)
			}
		})
	}
}

func TestDecidePrefilter(t *testing.T) {
	results := []serp.Result{
		{Title: "Unrelated page", URL: "https://example.com/a", Snippet: "nothing here"},
		{Title: "Golang channels tutorial", URL: "https://example.com/b", Snippet: "how channels work"},
		{Title: "Random facts", URL: "https://example.com/c", Snippet: "channels of a river"},
	}

	indices := decide.Prefilter("golang channels", results, 0)
	if len(indices) != len(results) {
		t.Fatalf("indices = %v, want all", indices)
	}
	if indices[0] != 1 {
		t.Errorf("top = %d, want 1", indices[0])
	}

	if capped := decide.Prefilter("golang channels", results, 2); len(capped) != 2 {
		t.Errorf("capped len = %d, want 2", len(capped))
	}
}

func TestDecideEmptyInputAbstains(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request to %s", r.URL.Path)
		io.WriteString(w, `{}`)
	}))
	t.Cleanup(server.Close)

	decider := newDecideDecider(t, server, decide.Options{})
	ctx := context.Background()

	if selection, err := decider.RankResults(ctx, "query", nil); err != nil || !selection.Abstained || selection.Index != -1 {
		t.Errorf("RankResults = %+v, %v", selection, err)
	}
	if selection, err := decider.SelectPassage(ctx, "query", nil); err != nil || !selection.Abstained || selection.Index != -1 {
		t.Errorf("SelectPassage = %+v, %v", selection, err)
	}
	if selection, err := decider.SelectSentence(ctx, "query", nil); err != nil || !selection.Abstained || selection.Index != -1 {
		t.Errorf("SelectSentence = %+v, %v", selection, err)
	}
}
