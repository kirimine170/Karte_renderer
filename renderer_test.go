package renderer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlainMarkdownRendering(t *testing.T) {
	html, _, err := RenderString(t.TempDir(), "# Hello\n\nThis is **bold**.")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, html, "<h1>Hello</h1>")
	assertContains(t, html, "<strong>bold</strong>")
}

func TestFrontMatterExtraction(t *testing.T) {
	html, fm, err := RenderString(t.TempDir(), "---\ntitle: My Page\ntheme: gaia\n---\n# Body")
	if err != nil {
		t.Fatal(err)
	}
	if fm.Title != "My Page" || fm.Theme != "gaia" {
		t.Fatalf("unexpected frontmatter: %+v", fm)
	}
	assertContains(t, html, "<title>My Page</title>")
	assertContains(t, html, "<h1>Body</h1>")
}

func TestInlineAndBlockKatexSyntax(t *testing.T) {
	html, _, err := RenderString(t.TempDir(), "Inline $x+1$\n\n$$$\ny=x^2\n$$$")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, html, `<span class="katex" data-katex="x+1">x+1</span>`)
	assertContains(t, html, `<div class="katex-display" data-katex="y=x^2">y=x^2</div>`)
}

func TestCSVImport(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "data.csv"), "Name,Score\nAda,10\n")
	html, _, err := RenderString(root, `@import(type="csv" path="data.csv")`)
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, html, "<table>")
	assertContains(t, html, "<th>Name</th>")
	assertContains(t, html, "<td>Ada</td>")
}

func TestMarkdownImport(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "part.md"), "## Imported\n\nText")
	html, _, err := RenderString(root, `@import(type="md" path="part.md")`)
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, html, "<h2>Imported</h2>")
	assertContains(t, html, "<p>Text</p>")
}

func TestLayoutFallbackOrder(t *testing.T) {
	root := t.TempDir()
	html, _, err := RenderString(root, "# Fallback")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, html, "<!doctype html>")

	theme := filepath.Join(root, "themes", "default")
	if err := os.MkdirAll(theme, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(theme, "layout.html"), "layout {{TITLE}} {{CONTENT}}")
	html, _, err = RenderString(root, "---\ntitle: Layout\n---\n# Body")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(html, "layout Layout ") {
		t.Fatalf("layout.html not used: %s", html)
	}

	writeFile(t, filepath.Join(theme, "preview.html"), "preview {{TITLE}} {{CONTENT}}")
	html, _, err = RenderString(root, "---\ntitle: Preview\n---\n# Body")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(html, "preview Preview ") {
		t.Fatalf("preview.html not preferred: %s", html)
	}
}

func TestMarpSlideRendering(t *testing.T) {
	html, fm, err := RenderString(t.TempDir(), "---\ntitle: Deck\nmarp: true\n---\n# One\n\n---\n\n# Two")
	if err != nil {
		t.Fatal(err)
	}
	if !fm.Marp {
		t.Fatalf("expected marp frontmatter")
	}
	if got := strings.Count(html, `class="marp-slide"`); got != 2 {
		t.Fatalf("slide count = %d; html=%s", got, html)
	}
	assertContains(t, html, "<h1>One</h1>")
	assertContains(t, html, "<h1>Two</h1>")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertContains(t *testing.T, s, sub string) {
	t.Helper()
	if !strings.Contains(s, sub) {
		t.Fatalf("expected %q in:\n%s", sub, s)
	}
}
