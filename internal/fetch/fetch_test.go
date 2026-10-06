package fetch

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGetHTML(t *testing.T) {
	const body = "<html><body>hello</body></html>"

	var gotUserAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserAgent = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, body)
	}))
	defer server.Close()

	page, err := New(Options{UserAgent: "sense-test/1.0"}).Get(context.Background(), server.URL)
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

func TestGetRejectsBadResponses(t *testing.T) {
	t.Run("non-2xx", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		_, err := New(Options{}).Get(context.Background(), server.URL)
		if err == nil || !strings.Contains(err.Error(), "404") {
			t.Fatalf("error = %v, want status 404", err)
		}
	})

	t.Run("non-html", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{}`)
		}))
		defer server.Close()

		_, err := New(Options{}).Get(context.Background(), server.URL)
		if err == nil || !strings.Contains(err.Error(), "application/json") {
			t.Fatalf("error = %v, want unsupported content type", err)
		}
	})
}

func TestGetFollowsRedirects(t *testing.T) {
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, "<html><body>final</body></html>")
	}))
	defer final.Close()

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL, http.StatusFound)
	}))
	defer redirect.Close()

	page, err := New(Options{MaxRedirects: 3}).Get(context.Background(), redirect.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if page.URL != final.URL {
		t.Errorf("URL = %q, want %q", page.URL, final.URL)
	}
}

func TestGetRedirectCap(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, server.URL+"/next", http.StatusFound)
	}))
	defer server.Close()

	_, err := New(Options{MaxRedirects: 2}).Get(context.Background(), server.URL)
	if err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("error = %v, want redirect limit error", err)
	}
}

func TestGetTruncatesBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, strings.Repeat("a", 100))
	}))
	defer server.Close()

	page, err := New(Options{MaxBytes: 10}).Get(context.Background(), server.URL)
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

func TestGetRejectsNonHTTPSchemes(t *testing.T) {
	urls := []string{
		"file:///etc/passwd",
		"ftp://example.com/page",
		"mailto:someone@example.com",
	}

	for _, rawURL := range urls {
		if _, err := New(Options{}).Get(context.Background(), rawURL); err == nil {
			t.Errorf("Get(%q) = nil error, want unsupported scheme", rawURL)
		}
	}
}

func TestGetThrottlesPerHost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, "<html><body>ok</body></html>")
	}))
	defer server.Close()

	const interval = 100 * time.Millisecond
	fetcher := New(Options{MinHostInterval: interval})

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
