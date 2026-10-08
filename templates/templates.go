package templates

import (
	"gavrh/sense/views"

	"html/template"
	"io"

	"github.com/labstack/echo/v5"
)

type Templates struct {
	Templates *template.Template
}

func (t *Templates) Render(
	C *echo.Context,
	W io.Writer,
	Name string,
	Data any,
) error {
	return t.Templates.ExecuteTemplate(W, Name, Data)
}

func NewTemplate() *Templates {
	funcs := template.FuncMap{
		"sub": func(a, b int) int { return a - b },
	}

	return &Templates{
		Templates: template.Must(template.New("views").Funcs(funcs).ParseFS(views.FS, "*.html")),
	}
}

const (
	Index   = "index"
	Result  = "result"
	Message = "message"
)
