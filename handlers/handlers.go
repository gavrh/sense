package handlers

import (
	"gavrh/sense/handlers/getHandlers"

	"github.com/labstack/echo/v5"
)

func HandleRequests(e *echo.Echo) {

	e.GET("/", func (c *echo.Context) error { return getHandlers.HandleGetDefault(c) })

}
