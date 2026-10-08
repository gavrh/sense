package tests

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"gavrh/sense/handlers/postHandlers"
	"gavrh/sense/internal/search"
	"gavrh/sense/templates"

	"github.com/labstack/echo/v5"
)

type stubSearchRunner struct {
	result search.Result
	err    error
}

func (s *stubSearchRunner) Search(ctx context.Context, query string) (search.Result, error) {
	return s.result, s.err
}

func newSearchHandler(runner postHandlers.SearchRunner) *echo.Echo {
	e := echo.New()
	e.Renderer = templates.NewTemplate()
	e.POST("/search", postHandlers.HandlePostSearch(runner))
	return e
}

func postSearchForm(t *testing.T, e *echo.Echo, query string) *httptest.ResponseRecorder {
	t.Helper()

	form := url.Values{"q": {query}}
	req := httptest.NewRequest(http.MethodPost, "/search", strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestHandleSearchSuccess(t *testing.T) {
	runner := &stubSearchRunner{result: search.Result{
		Answer:      "Paris is the capital.",
		SourceTitle: "France",
		SourceURL:   "https://example.com/france",
		Confidence:  0.9,
	}}

	rec := postSearchForm(t, newSearchHandler(runner), "capital of france")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Paris is the capital.") {
		t.Errorf("body missing the answer: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "https://example.com/france") {
		t.Errorf("body missing the source URL: %s", rec.Body.String())
	}
}

func TestHandleSearchEmptyQuery(t *testing.T) {
	rec := postSearchForm(t, newSearchHandler(&stubSearchRunner{}), "   ")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `class="message"`) {
		t.Errorf("body is not a message fragment: %s", rec.Body.String())
	}
}

func TestHandleSearchNoResults(t *testing.T) {
	runner := &stubSearchRunner{err: search.ErrNoResults}

	rec := postSearchForm(t, newSearchHandler(runner), "obscure query")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `class="message"`) {
		t.Errorf("body is not a message fragment: %s", rec.Body.String())
	}
}

func TestHandleSearchFailure(t *testing.T) {
	runner := &stubSearchRunner{err: errors.New("boom")}

	rec := postSearchForm(t, newSearchHandler(runner), "query")

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `class="message"`) {
		t.Errorf("body is not a message fragment: %s", rec.Body.String())
	}
}

func TestHandleSearchNotConfigured(t *testing.T) {
	rec := postSearchForm(t, newSearchHandler(nil), "query")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `class="message"`) {
		t.Errorf("body is not a message fragment: %s", rec.Body.String())
	}
}
