package getHandlers

import (
	"gavrh/sense/templates"

	// "context"
	// "fmt"
	"net/http"

	"github.com/labstack/echo/v5"
)

func HandleGetDefault(c *echo.Context) error {
	main := templates.NewMainTemplate("HOME")
	return c.Render(http.StatusOK, templates.Index, templates.NewIndexTemplate(main))
}
