package fetch

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	defaultTimeout      = 15 * time.Second
	defaultMaxBytes     = 2 << 20
	defaultMaxRedirects = 5
	defaultUserAgent    = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"
)

type Options struct {
	Timeout         time.Duration // per request; if <= 0 use 15s
	MaxBytes        int64         // response body cap; if <= 0 use 2 MiB
	MaxRedirects    int           // if <= 0 use 5
	UserAgent       string        // if empty use a desktop browser UA
	MinHostInterval time.Duration // min gap between requests to the same host; 0 = no throttle
}

type Page struct {
	URL         string // final URL after redirects
	StatusCode  int
	ContentType string
	Body        []byte
	Truncated   bool // true if the body exceeded MaxBytes
}

type Fetcher struct {
	client          *http.Client
	timeout         time.Duration
	maxBytes        int64
	userAgent       string
	minHostInterval time.Duration

	mu       sync.Mutex
	hostNext map[string]time.Time
}

func New(opts Options) *Fetcher {
	if opts.Timeout <= 0 {
		opts.Timeout = defaultTimeout
	}
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = defaultMaxBytes
	}
	if opts.MaxRedirects <= 0 {
		opts.MaxRedirects = defaultMaxRedirects
	}
	if opts.UserAgent == "" {
		opts.UserAgent = defaultUserAgent
	}

	return &Fetcher{
		client:          &http.Client{CheckRedirect: redirectGuard(opts.MaxRedirects)},
		timeout:         opts.Timeout,
		maxBytes:        opts.MaxBytes,
		userAgent:       opts.UserAgent,
		minHostInterval: opts.MinHostInterval,
		hostNext:        make(map[string]time.Time),
	}
}

func (f *Fetcher) Get(ctx context.Context, rawURL string) (*Page, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("fetch: parse url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("fetch: unsupported scheme %q", u.Scheme)
	}

	if err := f.waitForHost(ctx, u.Host); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("fetch: build request: %w", err)
	}
	req.Header.Set("User-Agent", f.userAgent)

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("fetch: server returned status %d", resp.StatusCode)
	}

	contentType, err := acceptedContentType(resp.Header.Get("Content-Type"))
	if err != nil {
		return nil, err
	}

	body, truncated, err := readCapped(resp.Body, f.maxBytes)
	if err != nil {
		return nil, err
	}

	return &Page{
		URL:         resp.Request.URL.String(),
		StatusCode:  resp.StatusCode,
		ContentType: contentType,
		Body:        body,
		Truncated:   truncated,
	}, nil
}

func (f *Fetcher) waitForHost(ctx context.Context, host string) error {
	if f.minHostInterval <= 0 {
		return nil
	}

	// Reserve the next slot before waiting so concurrent callers stay spaced out.
	f.mu.Lock()
	now := time.Now()
	next, ok := f.hostNext[host]
	if !ok || next.Before(now) {
		next = now
	}
	f.hostNext[host] = next.Add(f.minHostInterval)
	f.mu.Unlock()

	delay := time.Until(next)
	if delay <= 0 {
		return nil
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func redirectGuard(maxRedirects int) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
			return fmt.Errorf("fetch: redirect to unsupported scheme %q", req.URL.Scheme)
		}
		if len(via) > maxRedirects {
			return fmt.Errorf("fetch: stopped after %d redirects", maxRedirects)
		}
		return nil
	}
}

func acceptedContentType(header string) (string, error) {
	mediaType, _, err := mime.ParseMediaType(header)
	if err != nil {
		return "", fmt.Errorf("fetch: invalid content type %q", header)
	}
	if mediaType != "text/html" && mediaType != "application/xhtml+xml" {
		return "", fmt.Errorf("fetch: unsupported content type %q", mediaType)
	}
	return mediaType, nil
}

// readCapped reads at most maxBytes and reports whether the body was larger.
func readCapped(r io.Reader, maxBytes int64) ([]byte, bool, error) {
	body, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, false, fmt.Errorf("fetch: read body: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return body[:maxBytes], true, nil
	}
	return body, false, nil
}
