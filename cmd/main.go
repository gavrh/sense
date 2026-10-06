package main

import (
	"gavrh/sense/handlers"
	"gavrh/sense/internal/config"
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
	handlers.HandleRequests(e)

	if err := e.Start(cfg.Addr); err != nil {
		e.Logger.Error("failed to start server", "error", err)
	}
}
