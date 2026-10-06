package handlers

import (
	"gavrh/sense/handlers/getHandlers"

	"github.com/labstack/echo/v5"
)

func HandleRequests(e *echo.Echo) {
	e.GET("/", getHandlers.HandleGetDefault)
	e.GET("/:path", HandleGet)
}
