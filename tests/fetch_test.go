package tests

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gavrh/sense/internal/fetch"
)

func TestFetchGet(t *testing.T) {
	const body = "<html><body>hello</body></html>"

	var gotUserAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserAgent = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)

	page, err := fetch.New(fetch.Options{UserAgent: "sense-test/1.0"}).Get(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if page.URL != server.URL {
		t.Errorf("URL = %q, want %q", page.URL, server.URL)
	}
	if page.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200", page.StatusCode)
	}
	if page.ContentType != "text/html" {
		t.Errorf("ContentType = %q, want text/html", page.ContentType)
	}
	if string(page.Body) != body {
		t.Errorf("Body = %q, want %q", page.Body, body)
	}
	if page.Truncated {
		t.Error("Truncated = true, want false")
	}
	if gotUserAgent != "sense-test/1.0" {
		t.Errorf("User-Agent = %q, want sense-test/1.0", gotUserAgent)
	}
}

func TestFetchGetRejectsBadResponses(t *testing.T) {
	t.Run("non-2xx", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		t.Cleanup(server.Close)

		_, err := fetch.New(fetch.Options{}).Get(context.Background(), server.URL)
		if err == nil || !strings.Contains(err.Error(), "404") {
			t.Fatalf("error = %v, want status 404", err)
		}
	})

	t.Run("non-html", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{}`)
		}))
		t.Cleanup(server.Close)

		_, err := fetch.New(fetch.Options{}).Get(context.Background(), server.URL)
		if err == nil || !strings.Contains(err.Error(), "application/json") {
			t.Fatalf("error = %v, want unsupported content type", err)
		}
	})
}

func TestFetchGetRedirects(t *testing.T) {
	t.Run("follows redirect", func(t *testing.T) {
		final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			io.WriteString(w, "<html><body>final</body></html>")
		}))
		t.Cleanup(final.Close)

		redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, final.URL, http.StatusFound)
		}))
		t.Cleanup(redirect.Close)

		page, err := fetch.New(fetch.Options{MaxRedirects: 3}).Get(context.Background(), redirect.URL)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if page.URL != final.URL {
			t.Errorf("URL = %q, want %q", page.URL, final.URL)
		}
	})

	t.Run("caps redirects", func(t *testing.T) {
		var server *httptest.Server
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, server.URL+"/next", http.StatusFound)
		}))
		t.Cleanup(server.Close)

		_, err := fetch.New(fetch.Options{MaxRedirects: 2}).Get(context.Background(), server.URL)
		if err == nil || !strings.Contains(err.Error(), "redirect") {
			t.Fatalf("error = %v, want redirect limit error", err)
		}
	})
}

func TestFetchGetTruncatesBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, strings.Repeat("a", 100))
	}))
	t.Cleanup(server.Close)

	page, err := fetch.New(fetch.Options{MaxBytes: 10}).Get(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(page.Body) != 10 {
		t.Errorf("len(Body) = %d, want 10", len(page.Body))
	}
	if !page.Truncated {
		t.Error("Truncated = false, want true")
	}
}

func TestFetchGetRejectsNonHTTPSchemes(t *testing.T) {
	urls := []string{
		"file:///etc/passwd",
		"ftp://example.com/page",
		"mailto:someone@example.com",
	}

	for _, rawURL := range urls {
		if _, err := fetch.New(fetch.Options{}).Get(context.Background(), rawURL); err == nil {
			t.Errorf("Get(%q) = nil error, want unsupported scheme", rawURL)
		}
	}
}

func TestFetchThrottlesPerHost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, "<html><body>ok</body></html>")
	}))
	t.Cleanup(server.Close)

	const interval = 100 * time.Millisecond
	fetcher := fetch.New(fetch.Options{MinHostInterval: interval})

	start := time.Now()
	for i := 0; i < 2; i++ {
		if _, err := fetcher.Get(context.Background(), server.URL); err != nil {
			t.Fatalf("Get: %v", err)
		}
	}

	if elapsed := time.Since(start); elapsed < interval {
		t.Errorf("elapsed = %v, want at least %v", elapsed, interval)
	}
}
