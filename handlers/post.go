package handlers

import (
	"gavrh/sense/handlers/postHandlers"

	"github.com/labstack/echo/v5"
)

func HandlePost(runner postHandlers.SearchRunner) echo.HandlerFunc {
	return func(c *echo.Context) error {
		switch c.Param("path") {
		case "search":
			return postHandlers.HandlePostSearch(runner)(c)
		default:
			return postHandlers.HandlePostDefault(c)
		}
	}
}
