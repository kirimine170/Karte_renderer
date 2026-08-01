package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunAppliesBookPrintoutFlags(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "book.md")
	output := filepath.Join(root, "book.html")
	if err := os.WriteFile(input, []byte("# Book"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := run([]string{
		"--root", root,
		"--pdf-page-size", "B5",
		"--pdf-orientation", "portrait",
		"--pdf-margin", "18mm 15mm",
		"--pdf-inside-margin", "20mm",
		"--pdf-outside-margin", "14mm",
		"--pdf-header", "Book header",
		"--pdf-footer", "Book footer",
		"--pdf-page-numbers",
		"--pdf-chapter-start", "right",
		input, output,
	})
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	html := string(rendered)
	for _, want := range []string{"data-printout=\"B5\"", "size:B5 portrait", "margin-right:20mm", "counter(page)", "break-before:recto"} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected %q in:\n%s", want, html)
		}
	}
}

func TestRunCanDisableFrontMatterPageNumbers(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "book.md")
	output := filepath.Join(root, "book.html")
	if err := os.WriteFile(input, []byte("---\nprintout:\n  size: B5\n  pageNumbers: true\n---\n# Book"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"--root", root, "--pdf-page-numbers=false", input, output}); err != nil {
		t.Fatal(err)
	}
	rendered, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rendered), "counter(page)") {
		t.Fatalf("CLI false must override front matter pageNumbers:\n%s", rendered)
	}
}
