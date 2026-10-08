package tests

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"gavrh/sense/internal/jev"
)

const jevEmptyResponse = `{"model":"english","answers":{},"usage":{"input_tokens":0,"output_tokens":0}}`

func TestJevSystemOneRequest(t *testing.T) {
	t.Run("sends request with bearer token", func(t *testing.T) {
		var (
			gotMethod      string
			gotPath        string
			gotContentType string
			gotAuth        string
			gotBody        jev.Request
		)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotMethod = r.Method
			gotPath = r.URL.Path
			gotContentType = r.Header.Get("Content-Type")
			gotAuth = r.Header.Get("Authorization")
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				t.Errorf("decode request body: %v", err)
			}
			io.WriteString(w, jevEmptyResponse)
		}))
		t.Cleanup(server.Close)

		minConfidence := 0.5
		maxLen := 512
		headMaxLen := 192

		client := jev.New(server.URL, "secret", "english", 5*time.Second)
		_, err := client.SystemOne(context.Background(), jev.Request{
			State: "the state",
			Questions: map[string]jev.Question{
				"color": {
					Type:         "choice",
					Instructions: "Pick the colour",
					Criteria:     map[string]string{"red": "warm", "blue": "cool"},
				},
			},
			MinConfidence: &minConfidence,
			MaxLen:        &maxLen,
			HeadMaxLen:    &headMaxLen,
		})
		if err != nil {
			t.Fatalf("SystemOne: %v", err)
		}

		if gotMethod != http.MethodPost {
			t.Errorf("method = %q, want POST", gotMethod)
		}
		if gotPath != "/v1/systemone" {
			t.Errorf("path = %q, want /v1/systemone", gotPath)
		}
		if gotContentType != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", gotContentType)
		}
		if gotAuth != "Bearer secret" {
			t.Errorf("Authorization = %q, want Bearer secret", gotAuth)
		}
		if gotBody.State != "the state" {
			t.Errorf("state = %v, want the state", gotBody.State)
		}
		if gotBody.Model != "english" {
			t.Errorf("model = %q, want english", gotBody.Model)
		}
		if gotBody.Questions["color"].Type != "choice" {
			t.Errorf("question type = %q, want choice", gotBody.Questions["color"].Type)
		}
		if gotBody.MinConfidence == nil || *gotBody.MinConfidence != 0.5 {
			t.Errorf("min_confidence = %v, want 0.5", gotBody.MinConfidence)
		}
		if gotBody.MaxLen == nil || *gotBody.MaxLen != 512 {
			t.Errorf("max_len = %v, want 512", gotBody.MaxLen)
		}
		if gotBody.HeadMaxLen == nil || *gotBody.HeadMaxLen != 192 {
			t.Errorf("head_max_len = %v, want 192", gotBody.HeadMaxLen)
		}
	})

	t.Run("omits bearer token without key", func(t *testing.T) {
		var gotAuth string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			io.WriteString(w, jevEmptyResponse)
		}))
		t.Cleanup(server.Close)

		if _, err := jev.New(server.URL, "", "english", 5*time.Second).SystemOne(context.Background(), jev.Request{State: "x"}); err != nil {
			t.Fatalf("SystemOne: %v", err)
		}
		if gotAuth != "" {
			t.Errorf("Authorization = %q, want empty", gotAuth)
		}
	})
}

func TestJevResponseDecoding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{
			"model":"english",
			"answers":{
				"color":{"type":"choice","choice":"blue","probabilities":{"blue":0.91},"confidence":0.91,"action":{"act_probability":1.0}},
				"is_blue":{"type":"noul","noul":0.87},
				"score":{"type":"score","score":2,"answer_confidence":0.7}
			},
			"usage":{"input_tokens":10,"output_tokens":3},
			"routing":{"model":"english","reason":"best fit"}
		}`)
	}))
	t.Cleanup(server.Close)

	resp, err := jev.New(server.URL, "", "english", 5*time.Second).SystemOne(context.Background(), jev.Request{
		State:     "x",
		Questions: map[string]jev.Question{},
	})
	if err != nil {
		t.Fatalf("SystemOne: %v", err)
	}

	color, err := resp.Answers["color"].AsString()
	if err != nil || color != "blue" {
		t.Errorf("AsString = %q, %v; want blue, nil", color, err)
	}
	isBlue, err := resp.Answers["is_blue"].AsBool()
	if err != nil || !isBlue {
		t.Errorf("AsBool = %v, %v; want true, nil", isBlue, err)
	}
	score := resp.Answers["score"].Score
	if score == nil || *score != 2 {
		t.Errorf("score = %v, want 2", score)
	}

	if got := resp.Answers["color"].Confidence; got == nil || *got != 0.91 {
		t.Errorf("confidence = %v, want 0.91", got)
	}
	if got := resp.Answers["score"].AnswerConfidence; got == nil || *got != 0.7 {
		t.Errorf("answer_confidence = %v, want 0.7", got)
	}
	if resp.Usage.InputTokens != 10 || resp.Usage.OutputTokens != 3 {
		t.Errorf("usage = %+v", resp.Usage)
	}
	if resp.Routing["reason"] != "best fit" {
		t.Errorf("routing reason = %v", resp.Routing["reason"])
	}
}

func TestJevAPIErrors(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		wantDetail string
	}{
		{"structured", http.StatusUnauthorized, `{"detail":"missing bearer token"}`, "missing bearer token"},
		{"unstructured", http.StatusBadRequest, `plain text`, "plain text"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}))
			t.Cleanup(server.Close)

			_, err := jev.New(server.URL, "", "english", 5*time.Second).SystemOne(context.Background(), jev.Request{State: "x"})

			var apiErr *jev.APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error = %v, want *jev.APIError", err)
			}
			if apiErr.StatusCode != tc.status {
				t.Errorf("status = %d, want %d", apiErr.StatusCode, tc.status)
			}
			if apiErr.Detail != tc.wantDetail {
				t.Errorf("detail = %q, want %q", apiErr.Detail, tc.wantDetail)
			}
		})
	}
}

func TestJevRetriesOn503(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, `{"detail":"busy"}`)
			return
		}
		io.WriteString(w, jevEmptyResponse)
	}))
	t.Cleanup(server.Close)

	if _, err := jev.New(server.URL, "", "english", 5*time.Second).SystemOne(context.Background(), jev.Request{State: "x"}); err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	if attempts != 2 {
		t.Errorf("attempts = %d, want 2", attempts)
	}
}

func TestJevSystemOneBatch(t *testing.T) {
	var gotBody jev.BatchRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/systemone/batch" {
			t.Errorf("path = %q, want /v1/systemone/batch", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		io.WriteString(w, `{
			"results":[
				{"model":"english","answers":{"q":{"type":"noul","noul":0.9}},"usage":{"input_tokens":5,"output_tokens":1}}
			],
			"total_usage":{"input_tokens":5,"output_tokens":1}
		}`)
	}))
	t.Cleanup(server.Close)

	resp, err := jev.New(server.URL, "", "english", 5*time.Second).SystemOneBatch(context.Background(), jev.BatchRequest{
		States:    []any{"one", "two"},
		Questions: map[string]jev.Question{"q": {Type: "noul", Instructions: "Is it?"}},
	})
	if err != nil {
		t.Fatalf("SystemOneBatch: %v", err)
	}

	if len(gotBody.States) != 2 {
		t.Errorf("states = %v, want 2 entries", gotBody.States)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("results = %d, want 1", len(resp.Results))
	}
	if answer, err := resp.Results[0].Answers["q"].AsBool(); err != nil || !answer {
		t.Errorf("answer = %v, %v; want true, nil", answer, err)
	}
	if resp.TotalUsage.InputTokens != 5 || resp.TotalUsage.OutputTokens != 1 {
		t.Errorf("total_usage = %+v", resp.TotalUsage)
	}
}

func TestJevHealth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}
		if r.URL.Path != "/health" {
			t.Errorf("path = %q, want /health", r.URL.Path)
		}
		io.WriteString(w, `{"status":"ok","loaded":["english"],"device":"cpu","extra":1}`)
	}))
	t.Cleanup(server.Close)

	health, err := jev.New(server.URL, "", "english", 5*time.Second).Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if health.Status != "ok" {
		t.Errorf("status = %q, want ok", health.Status)
	}
	if len(health.Loaded) != 1 || health.Loaded[0] != "english" {
		t.Errorf("loaded = %v, want [english]", health.Loaded)
	}
	if health.Device != "cpu" {
		t.Errorf("device = %q, want cpu", health.Device)
	}
}

// TestLive exercises a real backend when JEV_BASE_URL is set.
func TestLive(t *testing.T) {
	baseURL := os.Getenv("JEV_BASE_URL")
	if baseURL == "" {
		t.Skip("JEV_BASE_URL not set")
	}

	client := jev.New(baseURL, os.Getenv("JEV_API_KEY"), "english", 30*time.Second)
	ctx := context.Background()

	if _, err := client.Health(ctx); err != nil {
		t.Fatalf("Health: %v", err)
	}

	resp, err := client.SystemOne(ctx, jev.Request{
		State: "The sky is blue.",
		Questions: map[string]jev.Question{
			"is_blue": {Type: "noul", Instructions: "Is the sky described as blue?"},
		},
	})
	if err != nil {
		t.Fatalf("SystemOne: %v", err)
	}

	answer, ok := resp.Answers["is_blue"]
	if !ok {
		t.Fatalf("missing answer for is_blue, got %+v", resp.Answers)
	}
	if _, err := answer.AsBool(); err != nil {
		t.Fatalf("answer not a noul: %v (%+v)", err, answer)
	}
}
