package renderer

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestConvertDocumentToHTML(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "docs", "page.md")
	output := filepath.Join(root, "build", "page.html")
	if err := os.MkdirAll(filepath.Dir(input), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, input, "---\ntitle: Page\n---\n# Hello\n\n![image](asset.png)")
	fm, err := ConvertFile(context.Background(), input, output, ConvertOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if fm.Title != "Page" {
		t.Fatalf("unexpected front matter: %+v", fm)
	}
	b, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	html := string(b)
	assertContains(t, html, "<h1>Hello</h1>")
	assertContains(t, html, `<base href="file://`)
	assertContains(t, html, "/docs/")
	assertContains(t, html, `id="karte-renderer-css"`)
	assertContains(t, html, "#c2b4ff")
}

func TestConvertDocumentWithNoCSS(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "page.md")
	output := filepath.Join(root, "page.html")
	writeFile(t, input, "# Plain")

	if _, err := ConvertFile(context.Background(), input, output, ConvertOptions{Root: root, NoCSS: true}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	html := string(b)
	if strings.Contains(html, `id="karte-renderer-css"`) || strings.Contains(html, "#c2b4ff") {
		t.Fatalf("CSS should be omitted:\n%s", html)
	}
}

func TestConvertDocumentWithCustomCSS(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "page.md")
	output := filepath.Join(root, "page.html")
	css := filepath.Join(root, "custom.css")
	writeFile(t, input, "# Custom")
	writeFile(t, css, "body { color: tomato; }")

	if _, err := ConvertFile(context.Background(), input, output, ConvertOptions{Root: root, CSS: css}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	html := string(b)
	assertContains(t, html, `id="karte-renderer-css"`)
	assertContains(t, html, "color: tomato")
	if strings.Contains(html, "#c2b4ff") {
		t.Fatalf("default CSS should be replaced:\n%s", html)
	}
}

func TestConvertRejectsCSSAndNoCSS(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "page.md")
	writeFile(t, input, "# Conflict")
	_, err := ConvertFile(context.Background(), input, filepath.Join(root, "page.html"), ConvertOptions{
		Root:  root,
		CSS:   filepath.Join(root, "custom.css"),
		NoCSS: true,
	})
	if err == nil || !strings.Contains(err.Error(), "cannot be used together") {
		t.Fatalf("expected conflicting CSS option error, got %v", err)
	}
}

func TestConvertMarpUsesOfficialCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell helper is Unix-only")
	}
	root := t.TempDir()
	input := filepath.Join(root, "slides.md")
	output := filepath.Join(root, "slides.pptx")
	writeFile(t, input, "---\nmarp: true\n---\n# Slide")
	binary := filepath.Join(root, "fake-marp")
	writeExecutable(t, binary, `#!/bin/sh
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--output" ]; then
    shift
    printf 'fake-pptx' > "$1"
    exit 0
  fi
  shift
done
exit 2
`)
	fm, err := ConvertFile(context.Background(), input, output, ConvertOptions{Root: root, Marp: MarpOptions{Binary: binary}})
	if err != nil {
		t.Fatal(err)
	}
	if !fm.Marp {
		t.Fatal("expected Marp front matter")
	}
	b, err := os.ReadFile(output)
	if err != nil || string(b) != "fake-pptx" {
		t.Fatalf("unexpected fake Marp output: %q, %v", b, err)
	}
}

func TestExportHTMLPDFWithChromiumCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell helper is Unix-only")
	}
	root := t.TempDir()
	htmlFile := filepath.Join(root, "page.html")
	output := filepath.Join(root, "page.pdf")
	writeFile(t, htmlFile, "<!doctype html><title>test</title>")
	binary := filepath.Join(root, "fake-chromium")
	writeExecutable(t, binary, `#!/bin/sh
for arg in "$@"; do
  case "$arg" in
    --print-to-pdf=*)
      output=${arg#*=}
      printf '%%PDF-1.4' > "$output"
      exit 0
      ;;
  esac
done
exit 2
`)
	if err := ExportHTMLPDF(context.Background(), htmlFile, output, PDFOptions{Engine: "chromium", Binary: binary}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(output)
	if err != nil || !strings.HasPrefix(string(b), "%PDF-") {
		t.Fatalf("unexpected PDF output: %q, %v", b, err)
	}
}

func TestConvertRejectsPPTXForPlainDocumentWithoutMarpCLI(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "page.md")
	writeFile(t, input, "# Page")
	_, err := ConvertFile(context.Background(), input, filepath.Join(root, "page.docx"), ConvertOptions{Root: root})
	if err == nil || !strings.Contains(err.Error(), "unsupported document output") {
		t.Fatalf("expected unsupported extension error, got %v", err)
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
