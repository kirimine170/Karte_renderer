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

func TestCSVImportSelectColumns(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "data.csv"), "Name,Score,Note\nAda,10,ok\n")
	html, _, err := RenderString(root, `@import(type="csv" path="data.csv" select="Name,Note")`)
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, html, "<th>Name</th>")
	assertContains(t, html, "<th>Note</th>")
	if strings.Contains(html, "<th>Score</th>") || strings.Contains(html, "<td>10</td>") {
		t.Fatalf("unselected CSV column rendered:\n%s", html)
	}
}

func TestMarpClassDirective(t *testing.T) {
	html, _, err := RenderString(t.TempDir(), "---\nmarp: true\n---\n<!-- _class: lead invert -->\n# Title")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, html, `class="marp-slide lead invert"`)
	if strings.Contains(html, "_class:") {
		t.Fatalf("slide directive should not be rendered:\n%s", html)
	}
}

func TestKatexNotProcessedInsideCode(t *testing.T) {
	html, _, err := RenderString(t.TempDir(), "`$x$`\n\n$x$")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, html, "<code>$x$</code>")
	assertContains(t, html, `<span class="katex" data-katex="x">x</span>`)
}

func TestExportPDFReportsMissingBinary(t *testing.T) {
	err := ExportPDFWithBinary(filepath.Join(t.TempDir(), "missing-wkhtmltopdf"), "in.html", "out.pdf")
	if err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("expected missing binary error, got %v", err)
	}
}

func TestGFMTablesListsLinksAndFootnotes(t *testing.T) {
	html, _, err := RenderString(t.TempDir(), `| Name | Score |
| --- | ---: |
| Ada | 10 |

- [x] done
- [ ] next

[Karte](https://example.com)

Footnote[^1]

[^1]: detail`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<table>", "<th>Name</th>", `type="checkbox"`, `href="https://example.com"`, `class="footnotes"`} {
		assertContains(t, html, want)
	}
}

func TestYAMLFrontMatterSupportsListsAndNestedData(t *testing.T) {
	_, fm, err := RenderString(t.TempDir(), `---
title: "Structured: metadata"
owners:
  - alice
  - bob
viewers: [carol]
settings:
  draft: false
  retries: 3
---
# Body`)
	if err != nil {
		t.Fatal(err)
	}
	if fm.Title != "Structured: metadata" || len(fm.Owners) != 2 || fm.Owners[1] != "bob" || len(fm.Viewers) != 1 {
		t.Fatalf("unexpected front matter: %+v", fm)
	}
	settings, ok := fm.Data["settings"].(map[string]interface{})
	if !ok || settings["draft"] != false || settings["retries"] != 3 {
		t.Fatalf("nested front matter was not preserved: %#v", fm.Data)
	}
}

func TestYAMLFrontMatterAcceptsLegacyCommaSeparatedPeople(t *testing.T) {
	_, fm, err := RenderString(t.TempDir(), "---\nowners: alice, bob\n---\nBody")
	if err != nil {
		t.Fatal(err)
	}
	if len(fm.Owners) != 2 || fm.Owners[0] != "alice" || fm.Owners[1] != "bob" {
		t.Fatalf("unexpected owners: %#v", fm.Owners)
	}
}

func TestHardWrap(t *testing.T) {
	html, _, err := RenderStringWithOptions(t.TempDir(), "first\nsecond", true)
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, html, "first<br>\nsecond")
}

func TestCyclicMarkdownImport(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"), `@import(type="md" path="b.md")`)
	writeFile(t, filepath.Join(root, "b.md"), `@import(type="md" path="a.md")`)
	_, _, err := RenderMarkdown(root, "a.md")
	if err == nil || !strings.Contains(err.Error(), "cyclic @import") {
		t.Fatalf("expected cyclic import error, got %v", err)
	}
}

func TestImportRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.md")
	writeFile(t, outside, "secret")
	if err := os.Symlink(outside, filepath.Join(root, "linked.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, _, err := RenderString(root, `@import(type="md" path="linked.md")`)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink escape error, got %v", err)
	}
}
