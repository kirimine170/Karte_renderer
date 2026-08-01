package renderer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrintoutFrontMatterSupportsScalarAndMapping(t *testing.T) {
	_, scalar, err := RenderString(t.TempDir(), "---\nprintout: B5\n---\n# Book")
	if err != nil {
		t.Fatal(err)
	}
	if scalar.Printout.Size != "B5" {
		t.Fatalf("scalar printout was not preserved: %+v", scalar.Printout)
	}

	_, mapped, err := RenderString(t.TempDir(), `---
printout:
  size: B5
  orientation: portrait
  insideMargin: 20mm
  outsideMargin: 14mm
  pageNumbers: true
  chapterStart: right
  expected_pages: 48
---
# Book`)
	if err != nil {
		t.Fatal(err)
	}
	if mapped.Printout.Size != "B5" || mapped.Printout.InsideMargin != "20mm" ||
		mapped.Printout.PageNumbers == nil || !*mapped.Printout.PageNumbers || mapped.Printout.ExpectedPages != 48 {
		t.Fatalf("mapped printout was not preserved: %+v", mapped.Printout)
	}
}

func TestResolvePrintoutOptionsUsesFieldByFieldPrecedence(t *testing.T) {
	enabled := true
	disabled := false
	frontMatter := PrintoutOptions{
		Size: "B5", Orientation: "portrait", Margin: "18mm 15mm",
		InsideMargin: "20mm", Header: "Front matter", PageNumbers: &enabled,
	}
	resolved, err := resolvePrintoutOptions(frontMatter, PDFOptions{
		Orientation: "landscape", Header: "Go API", PageNumbers: &disabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Size != "B5" || resolved.Margin != "18mm 15mm" || resolved.InsideMargin != "20mm" {
		t.Fatalf("unoverridden fields changed: %+v", resolved)
	}
	if resolved.Orientation != "landscape" || resolved.Header != "Go API" || resolved.PageNumbers == nil || *resolved.PageNumbers {
		t.Fatalf("explicit fields did not override front matter: %+v", resolved)
	}
}

func TestConvertB5BookPrintoutFixture(t *testing.T) {
	root, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "b5-book.html")
	if _, err := ConvertFile(context.Background(), filepath.Join(root, "testdata", "printout", "b5-book.md"), output, ConvertOptions{Root: root}); err != nil {
		t.Fatal(err)
	}
	rendered, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	html := string(rendered)
	for _, want := range []string{
		`<html data-printout="B5"`,
		`id="karte-printout-options"`,
		`@page{size:B5 portrait;margin:18mm 15mm;@top-center{content:"廣井きくり統計学";}@bottom-center{content:"第1章";}}`,
		`@page:left{margin-left:14mm;margin-right:20mm;@bottom-left{content:counter(page);}}`,
		`@page:right{margin-right:14mm;margin-left:20mm;@bottom-right{content:counter(page);}}`,
		`@media print{h1{break-before:recto;}}`,
	} {
		assertContains(t, html, want)
	}
}

func TestPrintoutRunningTextCannotTerminateStyleElement(t *testing.T) {
	document := applyPrintoutOptions("<!doctype html><html><head></head><body></body></html>", PrintoutOptions{
		Header: `Title</style><script>alert(1)</script>`,
	})
	if strings.Contains(document, `Title</style><script>`) {
		t.Fatalf("running text terminated the injected style element: %s", document)
	}
	assertContains(t, document, `Title\3c /style>\3c script>alert(1)\3c /script>`)
}

func TestConvertPrintoutExplicitOptionsOverrideFrontMatter(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "book.md")
	output := filepath.Join(root, "book.html")
	writeFile(t, input, "---\nprintout:\n  size: B5\n  orientation: portrait\n---\n# Book")
	pageNumbers := true
	_, err := ConvertFile(context.Background(), input, output, ConvertOptions{
		Root: root,
		PDF:  PDFOptions{PageSize: "A5", Orientation: "landscape", PageNumbers: &pageNumbers},
	})
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	html := string(rendered)
	assertContains(t, html, `<html data-printout="A5"`)
	assertContains(t, html, `@page{size:A5 landscape;}`)
	assertContains(t, html, `counter(page)`)
}

func TestConvertRejectsUnsafePrintoutValues(t *testing.T) {
	for _, test := range []struct {
		name, frontMatter, want string
	}{
		{"size", "size: Tabloid", "invalid printout page size"},
		{"orientation", "orientation: upside-down", "invalid printout orientation"},
		{"margin", `margin: "12mm; color: red"`, "invalid printout margin length"},
		{"chapter", "chapterStart: tomorrow", "invalid chapter start"},
		{"expected pages", "expected_pages: -1", "invalid expected_pages"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			input := filepath.Join(root, "book.md")
			writeFile(t, input, "---\nprintout:\n  "+test.frontMatter+"\n---\n# Book")
			_, err := ConvertFile(context.Background(), input, filepath.Join(root, "book.html"), ConvertOptions{Root: root})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}
		})
	}
}

func TestConvertWithoutPrintoutOptionsDoesNotInjectOverride(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "plain.md")
	output := filepath.Join(root, "plain.html")
	writeFile(t, input, "# Plain")
	if _, err := ConvertFile(context.Background(), input, output, ConvertOptions{Root: root}); err != nil {
		t.Fatal(err)
	}
	rendered, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rendered), "karte-printout-options") || strings.Contains(string(rendered), "<html data-printout=") {
		t.Fatalf("plain conversion should retain the built-in stylesheet only:\n%s", rendered)
	}
}

func TestPrepareHTMLPDFInputAppliesGoAPIOptionsWithoutMutatingSource(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "source.html")
	original := "<!doctype html><html><head></head><body>Book</body></html>"
	writeFile(t, input, original)
	prepared, cleanup, err := prepareHTMLPDFInput(input, PDFOptions{PageSize: "B5", Orientation: "landscape"})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if prepared == input {
		t.Fatal("printout options should use a temporary HTML input")
	}
	rendered, err := os.ReadFile(prepared)
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, string(rendered), "size:B5 landscape")
	source, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	if string(source) != original {
		t.Fatalf("source HTML was mutated: %s", source)
	}
}

func TestApplyPrintoutOptionsReplacesExistingMode(t *testing.T) {
	document := `<html lang="ja" data-printout="infinite"><head></head><body></body></html>`
	got := applyPrintoutOptions(document, PrintoutOptions{Size: "B5"})
	assertContains(t, got, `<html lang="ja" data-printout="B5">`)
	if strings.Count(got, "data-printout=") != 1 {
		t.Fatalf("data-printout attribute was duplicated: %s", got)
	}
}
