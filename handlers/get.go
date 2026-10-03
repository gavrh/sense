package handlers

import (
	"gavrh/sense/handlers/getHandlers"

	"github.com/labstack/echo/v5"
)

func HandleGet(c *echo.Context) error {

    path := c.Param("path")

    switch path {
        // temp while has no favicon.ico
        case "favicon.ico":
            return nil

    }
    
    return getHandlers.HandleGetDefault(c)
}
