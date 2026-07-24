package renderer

import (
	"os"
	"strings"
	"testing"
)

func TestKaTeXRuntimeEmbeddedOnlyForMath(t *testing.T) {
	withMath, _, err := RenderString(t.TempDir(), `Inline $x^2$`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`id="karte-katex-css"`,
		`id="karte-katex-library"`,
		`id="karte-katex-bootstrap"`,
		`data-katex-version="0.18.1"`,
		`data:font/woff2;base64,`,
		`katex.render(expression,element`,
	} {
		assertContains(t, withMath, want)
	}
	if strings.Contains(withMath, "url(fonts/") {
		t.Fatal("KaTeX CSS contains an unresolved font path")
	}

	withoutMath, _, err := RenderString(t.TempDir(), "# No math")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(withoutMath, "karte-katex-") {
		t.Fatal("KaTeX runtime should be omitted when the document has no math")
	}
}

func TestKaTeXRuntimeInjectedIntoLayoutWithoutPlaceholder(t *testing.T) {
	root := t.TempDir()
	theme := root + "/themes/default"
	if err := os.MkdirAll(theme, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, theme+"/preview.html", "<html><head><title>{{TITLE}}</title></head><body>{{CONTENT}}</body></html>")

	rendered, _, err := RenderString(root, `$x$`)
	if err != nil {
		t.Fatal(err)
	}
	headEnd := strings.Index(rendered, "</head>")
	runtime := strings.Index(rendered, `id="karte-katex-library"`)
	if runtime < 0 || headEnd < 0 || runtime > headEnd {
		t.Fatalf("KaTeX runtime was not injected into head: %s", rendered)
	}
}
