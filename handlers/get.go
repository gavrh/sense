package handlers

import (
	"net/http"

	"github.com/labstack/echo/v5"
)

func HandleGet(c *echo.Context) error {
	// temp while we have no favicon.ico
	if c.Param("path") == "favicon.ico" {
		return nil
	}

	return c.Redirect(http.StatusSeeOther, "/")
}
