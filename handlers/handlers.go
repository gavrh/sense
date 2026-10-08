package handlers

import (
	"gavrh/sense/handlers/getHandlers"
	"gavrh/sense/handlers/postHandlers"

	"github.com/labstack/echo/v5"
)

func HandleRequests(e *echo.Echo, runner postHandlers.SearchRunner) {
	e.GET("/", getHandlers.HandleGetDefault)
	e.GET("/:path", HandleGet)
	e.POST("/", postHandlers.HandlePostDefault)
	e.POST("/:path", HandlePost(runner))
}
