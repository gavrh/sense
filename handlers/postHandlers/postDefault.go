package postHandlers

import (
	"github.com/labstack/echo/v5"
)

func HandlePostDefault(c *echo.Context) error {
	return echo.ErrNotFound
}
