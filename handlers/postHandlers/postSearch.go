package postHandlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"gavrh/sense/internal/search"
	"gavrh/sense/templates"

	"github.com/labstack/echo/v5"
)

type SearchRunner interface {
	Search(ctx context.Context, query string) (search.Result, error)
}

func HandlePostSearch(runner SearchRunner) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if runner == nil {
			return renderMessage(c, http.StatusServiceUnavailable, "Search is not configured.")
		}

		query := strings.TrimSpace(c.FormValue("q"))
		if query == "" {
			return renderMessage(c, http.StatusBadRequest, "Enter a query.")
		}

		result, err := runner.Search(c.Request().Context(), query)
		switch {
		case errors.Is(err, search.ErrNoResults):
			return renderMessage(c, http.StatusOK, "No results found.")
		case errors.Is(err, search.ErrNoAnswer):
			return renderMessage(c, http.StatusOK, "No answer found.")
		case err != nil:
			return renderMessage(c, http.StatusBadGateway, "Search failed.")
		}

		return c.Render(http.StatusOK, templates.Result, templates.NewResultTemplate(
			result.SourceTitle,
			result.SourceURL,
			result.Answer,
			result.Confidence,
		))
	}
}

func renderMessage(c *echo.Context, status int, message string) error {
	return c.Render(status, templates.Message, templates.NewMessageTemplate(message))
}
