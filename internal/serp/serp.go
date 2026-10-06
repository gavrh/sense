package serp

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const (
	searchPath       = "/search"
	defaultTopN      = 10
	defaultTimeout   = 10 * time.Second
	defaultUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
)

type Result struct {
	Title   string
	URL     string
	Snippet string
}

type Options struct {
	Endpoint  string
	TopN      int
	Timeout   time.Duration
	UserAgent string
}

type Searcher struct {
	base      *url.URL
	topN      int
	timeout   time.Duration
	userAgent string
	http      *http.Client
}

func New(opts Options) (*Searcher, error) {
	if strings.TrimSpace(opts.Endpoint) == "" {
		return nil, fmt.Errorf("serp: endpoint is required")
	}

	base, err := url.Parse(opts.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("serp: parse endpoint: %w", err)
	}

	topN := opts.TopN
	if topN <= 0 {
		topN = defaultTopN
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	userAgent := opts.UserAgent
	if userAgent == "" {
		userAgent = defaultUserAgent
	}

	return &Searcher{
		base:      base,
		topN:      topN,
		timeout:   timeout,
		userAgent: userAgent,
		http:      &http.Client{},
	}, nil
}

func (s *Searcher) Search(ctx context.Context, query string) ([]Result, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	target := *s.base
	target.Path = strings.TrimRight(target.Path, "/") + searchPath
	params := target.Query()
	params.Set("q", query)
	target.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("serp: build request: %w", err)
	}
	req.Header.Set("User-Agent", s.userAgent)

	resp, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("serp: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("serp: server returned status %d", resp.StatusCode)
	}
	if !isHTML(resp.Header.Get("Content-Type")) {
		return nil, fmt.Errorf("serp: unexpected content type %q", resp.Header.Get("Content-Type"))
	}

	results, err := parseResults(resp.Body)
	if err != nil {
		return nil, err
	}
	return s.clean(results), nil
}

// clean resolves relative links, drops unusable entries, dedupes by URL and
// caps the list, keeping the original result order.
func (s *Searcher) clean(results []Result) []Result {
	seen := make(map[string]bool)
	cleaned := make([]Result, 0, len(results))

	for _, result := range results {
		resolved, ok := resolveURL(s.base, result.URL)
		if !ok {
			continue
		}

		key := strings.ToLower(normalizeURL(resolved))
		if seen[key] {
			continue
		}
		seen[key] = true

		result.URL = resolved
		cleaned = append(cleaned, result)
		if len(cleaned) == s.topN {
			break
		}
	}

	return cleaned
}

func isHTML(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	return mediaType == "text/html" || mediaType == "application/xhtml+xml"
}

// parseResults extracts raw results from a SearxNG HTML page. All engine-specific
// selectors live here so a different search engine can be swapped in later.
func parseResults(r io.Reader) ([]Result, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, fmt.Errorf("serp: parse html: %w", err)
	}

	containers := doc.Find("article.result")
	if containers.Length() == 0 {
		containers = doc.Find(".result")
	}

	var results []Result
	containers.Each(func(_ int, container *goquery.Selection) {
		title, href := resultLink(container)
		if title == "" || href == "" {
			return
		}

		results = append(results, Result{
			Title:   title,
			URL:     href,
			Snippet: resultSnippet(container),
		})
	})

	return results, nil
}

func resultLink(container *goquery.Selection) (string, string) {
	link := container.Find("h3 a").First()
	if link.Length() == 0 {
		link = container.Find("a.url").First()
	}
	if link.Length() == 0 {
		return "", ""
	}
	return strings.TrimSpace(link.Text()), strings.TrimSpace(link.AttrOr("href", ""))
}

func resultSnippet(container *goquery.Selection) string {
	snippet := container.Find("p.content").First()
	if snippet.Length() == 0 {
		snippet = container.Find(".content").First()
	}
	return strings.TrimSpace(snippet.Text())
}

func resolveURL(base *url.URL, href string) (string, bool) {
	ref, err := url.Parse(href)
	if err != nil {
		return "", false
	}

	resolved := base.ResolveReference(ref)
	if resolved.Host == "" || (resolved.Scheme != "http" && resolved.Scheme != "https") {
		return "", false
	}
	return resolved.String(), true
}

func normalizeURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}

	parsed.Fragment = ""
	parsed.RawFragment = ""
	return parsed.String()
}
