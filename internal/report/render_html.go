package report

import (
	"embed"
	"html/template"
	"io"
)

//go:embed template.html
var templateFS embed.FS

var htmlTemplate = template.Must(template.ParseFS(templateFS, "template.html"))

// RenderHTML writes r as a single self-contained HTML page to w (inline
// CSS only, no external requests -- see PLAN.md's "no external CDN"
// requirement). html/template (not text/template) is used deliberately:
// Report data ultimately originates from logged Bash commands, which are
// untrusted strings, so every value written into the page must be
// HTML-escaped.
func RenderHTML(w io.Writer, r Report) error {
	return htmlTemplate.Execute(w, r)
}
