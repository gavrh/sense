package main

import (
	"gavrh/sense/handlers"
	"gavrh/sense/templates"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
)

func main() {

	e := echo.New()
	e.Use(middleware.RequestLogger())
	e.Use(middleware.Recover())
	e.IPExtractor = echo.ExtractIPFromXFFHeader()

	e.Static("/static/assets", "assets")
	e.Static("/static/css", "css")

	e.Renderer = templates.NewTemplate()
	handlers.HandleRequests(e)
	
	if err := e.Start(":2727"); err != nil {
		e.Logger.Error("failed to start server", "error", err)
	}
}
