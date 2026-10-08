package main

import (
	"gavrh/sense/handlers"
	"gavrh/sense/handlers/postHandlers"
	"gavrh/sense/internal/config"
	"gavrh/sense/internal/decide"
	"gavrh/sense/internal/fetch"
	"gavrh/sense/internal/jev"
	"gavrh/sense/internal/search"
	"gavrh/sense/internal/serp"
	"gavrh/sense/templates"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
)

func main() {
	cfg := config.Load()

	e := echo.New()
	e.Use(middleware.RequestLogger())
	e.Use(middleware.Recover())
	e.IPExtractor = echo.ExtractIPFromXFFHeader()

	e.Renderer = templates.NewTemplate()

	runner, err := newSearchRunner(cfg)
	if err != nil {
		e.Logger.Warn("search backend disabled", "error", err)
	}

	handlers.HandleRequests(e, runner)

	if err := e.Start(cfg.Addr); err != nil {
		e.Logger.Error("failed to start server", "error", err)
	}
}

func newSearchRunner(cfg config.Config) (postHandlers.SearchRunner, error) {
	client := jev.New(cfg.JevBaseURL, cfg.JevAPIKey, cfg.JevModel, cfg.RequestTimeout)

	searcher, err := serp.New(serp.Options{
		Endpoint: cfg.SerpURL,
		TopN:     cfg.TopN,
		Timeout:  cfg.RequestTimeout,
	})
	if err != nil {
		return nil, err
	}

	fetcher := fetch.New(fetch.Options{Timeout: cfg.RequestTimeout})
	decider := decide.New(client, decide.Options{Model: cfg.JevModel})

	return search.New(searcher, fetcher, decider, search.Options{TopN: cfg.TopN}), nil
}
