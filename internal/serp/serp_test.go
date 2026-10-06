package serp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()

	body, err := os.ReadFile("testdata/searxng.html")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(body)
	}))
	t.Cleanup(server.Close)

	return server
}

func newSearcher(t *testing.T, endpoint string, topN int) *Searcher {
	t.Helper()

	searcher, err := New(Options{Endpoint: endpoint, TopN: topN, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return searcher
}

func TestSearchRequest(t *testing.T) {
	var (
		gotMethod string
		gotPath   string
		gotQuery  string
		gotUA     string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotQuery = r.URL.Query().Get("q")
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, "<html></html>")
	}))
	defer server.Close()

	searcher, err := New(Options{Endpoint: server.URL, UserAgent: "sense-test/1.0"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := searcher.Search(context.Background(), "hello world"); err != nil {
		t.Fatalf("Search: %v", err)
	}

	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/search" {
		t.Errorf("path = %q, want /search", gotPath)
	}
	if gotQuery != "hello world" {
		t.Errorf("q = %q, want hello world", gotQuery)
	}
	if gotUA != "sense-test/1.0" {
		t.Errorf("User-Agent = %q, want sense-test/1.0", gotUA)
	}
}

func TestSearchParsesFixture(t *testing.T) {
	server := fixtureServer(t)
	searcher := newSearcher(t, server.URL, 10)

	results, err := searcher.Search(context.Background(), "query")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	want := []Result{
		{Title: "First Result", URL: "https://example.com/first", Snippet: "The first snippet."},
		{Title: "Relative Result", URL: server.URL + "/relative/page", Snippet: "The relative snippet."},
		{Title: "Third Result", URL: "https://third.example.org/page", Snippet: "The third snippet."},
		{Title: "Fourth Result", URL: "https://fourth.example.org/page", Snippet: "The fourth snippet."},
	}

	if len(results) != len(want) {
		t.Fatalf("results = %+v, want %d entries", results, len(want))
	}
	for i, expected := range want {
		if results[i] != expected {
			t.Errorf("result[%d] = %+v, want %+v", i, results[i], expected)
		}
	}
}

func TestSearchCapsTopN(t *testing.T) {
	server := fixtureServer(t)
	searcher := newSearcher(t, server.URL, 2)

	results, err := searcher.Search(context.Background(), "query")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("results = %+v, want 2 entries", results)
	}
	if results[0].Title != "First Result" || results[1].Title != "Relative Result" {
		t.Errorf("titles = %q, %q", results[0].Title, results[1].Title)
	}
}

func TestSearchErrors(t *testing.T) {
	cases := []struct {
		name        string
		status      int
		contentType string
	}{
		{"status", http.StatusInternalServerError, "text/html"},
		{"content type", http.StatusOK, "application/json"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
				io.WriteString(w, "{}")
			}))
			defer server.Close()

			searcher := newSearcher(t, server.URL, 10)
			if _, err := searcher.Search(context.Background(), "query"); err == nil {
				t.Fatal("Search succeeded, want error")
			}
		})
	}
}

func TestNewRequiresEndpoint(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Fatal("New succeeded, want error")
	}
}

func TestParseResultsFallbacks(t *testing.T) {
	html := `<html><body>
		<div class="result">
			<a class="url" href="/legacy">Legacy Title</a>
			<div class="content">Legacy snippet.</div>
		</div>
	</body></html>`

	results, err := parseResults(strings.NewReader(html))
	if err != nil {
		t.Fatalf("parseResults: %v", err)
	}

	want := Result{Title: "Legacy Title", URL: "/legacy", Snippet: "Legacy snippet."}
	if len(results) != 1 || results[0] != want {
		t.Errorf("results = %+v, want %+v", results, want)
	}
}

func TestParseResultsError(t *testing.T) {
	if _, err := parseResults(iotest.ErrReader(errors.New("read failed"))); err == nil {
		t.Fatal("parseResults succeeded, want error")
	}
}
