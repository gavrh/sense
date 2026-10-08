package tests

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gavrh/sense/handlers/getHandlers"
	"gavrh/sense/templates"

	"github.com/labstack/echo/v5"
)

func TestIndexWiresSearchForm(t *testing.T) {
	e := echo.New()
	e.Renderer = templates.NewTemplate()
	e.GET("/", getHandlers.HandleGetDefault)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	body := rec.Body.String()
	for _, want := range []string{`hx-post="/search"`, `id="results"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q: %s", want, body)
		}
	}
}
