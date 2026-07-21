package renderer

import (
	_ "embed"
	"strings"
)

//go:embed assets/default.css
var defaultDocumentCSS string

func documentStyle(css string) string {
	if strings.TrimSpace(css) == "" {
		return ""
	}
	return "<style id=\"karte-renderer-css\">\n" + css + "\n</style>"
}
