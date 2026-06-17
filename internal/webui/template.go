package webui

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"io/fs"
)

//go:embed assets/login.html assets/login.js assets/error_fragment.html
var assets embed.FS

// Templates holds the parsed HTML templates used by the HTTP handlers.
type Templates struct {
	login         *template.Template
	errorFragment *template.Template
}

// Load parses the embedded templates. It should be called from main() so that
// template errors are reported gracefully instead of panicking at startup.
func Load() (*Templates, error) {
	loginTmpl, err := template.ParseFS(assets, "assets/login.html")
	if err != nil {
		return nil, fmt.Errorf("parse login template: %w", err)
	}
	errTmpl, err := template.ParseFS(assets, "assets/error_fragment.html")
	if err != nil {
		return nil, fmt.Errorf("parse error fragment template: %w", err)
	}
	return &Templates{login: loginTmpl, errorFragment: errTmpl}, nil
}

// AssetFS exposes the embedded static assets (e.g. login.js) for direct
// serving via http.FileServer or http.ServeFileFS.
func AssetFS() fs.FS {
	return assets
}

// LoginData is passed to the login template.
type LoginData struct {
	BasePath string
	CSRF     string
	Error    string
	ReturnTo string
}

// ExecuteLogin renders the login page into the given writer.
func (t *Templates) ExecuteLogin(w io.Writer, data LoginData) error {
	return t.login.Execute(w, data)
}

// ExecuteErrorFragment renders a small HTML fragment for htmx error swap.
func (t *Templates) ExecuteErrorFragment(w io.Writer, msg string) error {
	return t.errorFragment.Execute(w, LoginData{Error: msg})
}
