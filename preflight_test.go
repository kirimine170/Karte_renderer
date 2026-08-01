package renderer

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParsePDFInfo(t *testing.T) {
	pages, width, height, err := parsePDFInfo("Title: Book\nPages:          48\nPage size:      498.96 x 708.96 pts (B5)\n")
	if err != nil {
		t.Fatal(err)
	}
	if pages != 48 || width != 498.96 || height != 708.96 {
		t.Fatalf("unexpected pdfinfo values: %d %.2f %.2f", pages, width, height)
	}
}

func TestParsePDFFonts(t *testing.T) {
	output := `name                                 type              encoding         emb sub uni object ID
------------------------------------ ----------------- ---------------- --- --- --- ---------
AAAAAA+NotoSansJP                    CID TrueType      Identity-H       yes yes yes      5  0
Helvetica                            Type 1            WinAnsi          no  no  no       8  0
`
	fonts, unembedded := parsePDFFonts(output)
	if fonts != 2 || len(unembedded) != 1 || unembedded[0] != "Helvetica" {
		t.Fatalf("unexpected font result: %d %#v", fonts, unembedded)
	}
}

func TestParseOverflowProbe(t *testing.T) {
	issues, err := parseOverflowProbe(`<meta name="karte-preflight-overflow" content="%5B%22table%22%2C%22pre.wide%22%5D">`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(issues, ",") != "table,pre.wide" {
		t.Fatalf("unexpected overflow issues: %#v", issues)
	}
}

func TestHTMLPreflightDetectsMissingAssetsAndRawTeX(t *testing.T) {
	root := t.TempDir()
	htmlFile := filepath.Join(root, "book.html")
	writeFile(t, htmlFile, `<!doctype html><html><head><base href="`+fileURL(root)+`/"></head><body><img src="missing.svg"><p>$$$</p></body></html>`)
	assets := checkHTMLAssets(htmlFile)
	if assets.Passed || !strings.Contains(assets.Detail, "missing.svg") {
		t.Fatalf("missing asset was not reported: %+v", assets)
	}
	raw := checkHTMLRawTeX(htmlFile)
	if raw.Passed || !strings.Contains(raw.Detail, "$$$") {
		t.Fatalf("raw TeX was not reported: %+v", raw)
	}
}

func TestPreflightReportPathCannotOverwriteProtectedFiles(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "book.md")
	pdf := filepath.Join(root, "book.pdf")
	writeFile(t, input, "# Book")
	writeFile(t, pdf, "%PDF-1.4")
	for _, protected := range []string{input, pdf} {
		if _, err := validatePreflightReportPath(protected, input, pdf); err == nil || !strings.Contains(err.Error(), "must not overwrite") {
			t.Fatalf("expected protected-path error for %s, got %v", protected, err)
		}
	}
}

func TestRunDocumentPreflightRejectsNegativeExpectedPages(t *testing.T) {
	_, err := runDocumentPreflight(context.Background(), "book.html", "book.pdf", PreflightOptions{ExpectedPages: -1})
	if err == nil || !strings.Contains(err.Error(), "invalid expected pages") {
		t.Fatalf("expected negative page-count error, got %v", err)
	}
}

func TestWindowsFileURLPreservesUNCHost(t *testing.T) {
	parsed, err := url.Parse("file://server/share/image.png")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := filePathFromURL(parsed, "windows"), `\\server\share\image.png`; got != want {
		t.Fatalf("UNC URL path = %q, want %q", got, want)
	}
}

func TestPageSizePointsUsesCSSISOB4(t *testing.T) {
	width, height, ok := pageSizePoints("B4")
	if !ok || width != 708.66 || height != 1000.63 {
		t.Fatalf("unexpected B4 dimensions: %.2f x %.2f, ok=%v", width, height, ok)
	}
}

func TestConvertPreflightValidatesDefaultA4PageSize(t *testing.T) {
	requireShell(t)
	root := t.TempDir()
	input := filepath.Join(root, "book.md")
	output := filepath.Join(root, "book.pdf")
	writeFile(t, input, "# Book")
	pdfBinary := filepath.Join(root, "chromium-pdf")
	writeFile(t, pdfBinary, `#!/bin/sh
for arg in "$@"; do
  case "$arg" in
    --print-to-pdf=*)
      output=${arg#*=}
      printf '%%PDF-1.4\n%%%%EOF\n' > "$output"
      exit 0
      ;;
  esac
done
exit 2
`)
	if err := os.Chmod(pdfBinary, 0o755); err != nil {
		t.Fatal(err)
	}
	options := fakePreflightOptions(t, root)
	options.ReportPath = filepath.Join(root, "report.json")
	options.PDFInfoBinary = fakeTool(t, root, "pdfinfo-letter", "Pages: 1\nPage size: 612 x 792 pts\n")
	_, err := ConvertFile(context.Background(), input, output, ConvertOptions{
		Root:      root,
		PDF:       PDFOptions{Engine: "chromium", Binary: pdfBinary},
		Preflight: options,
	})
	var preflightErr *PreflightError
	if !errors.As(err, &preflightErr) || !strings.Contains(err.Error(), "expected A4 portrait") {
		t.Fatalf("expected default A4 page-size failure, got %v", err)
	}
}

func TestRunDocumentPreflightWritesPassingReport(t *testing.T) {
	requireShell(t)
	root := t.TempDir()
	htmlFile := filepath.Join(root, "book.html")
	pdfFile := filepath.Join(root, "book.pdf")
	reportFile := filepath.Join(root, "report.json")
	writeFile(t, htmlFile, "<!doctype html><html><head></head><body><p>Book</p></body></html>")
	writeFile(t, pdfFile, "%PDF-fake")
	options := fakePreflightOptions(t, root)
	options.ExpectedPages = 48
	options.ExpectedPageSize = "B5"
	options.ExpectedOrientation = "portrait"
	options.ReportPath = reportFile

	report, err := runDocumentPreflight(context.Background(), htmlFile, pdfFile, options)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed || len(report.Checks) != 7 {
		t.Fatalf("unexpected report: %+v", report)
	}
	assertReportFile(t, reportFile, true)
}

func TestRunDocumentPreflightAggregatesFailuresAndKeepsPDF(t *testing.T) {
	requireShell(t)
	root := t.TempDir()
	htmlFile := filepath.Join(root, "book.html")
	pdfFile := filepath.Join(root, "book.pdf")
	reportFile := filepath.Join(root, "report.json")
	writeFile(t, htmlFile, "<!doctype html><html><head></head><body><p>Book</p></body></html>")
	writeFile(t, pdfFile, "%PDF-fake")
	options := fakePreflightOptions(t, root)
	options.ExpectedPages = 48
	options.ExpectedPageSize = "B5"
	options.ReportPath = reportFile
	options.PDFInfoBinary = fakeTool(t, root, "pdfinfo-fail", "Pages: 47\nPage size: 595.28 x 841.89 pts\n")
	options.PDFFontsBinary = fakeTool(t, root, "pdffonts-fail", "Helvetica Type 1 WinAnsi no no no 8 0\n")
	options.PDFToTextBinary = fakeTool(t, root, "pdftotext-fail", "visible \\\\begin{align}\n")

	report, err := runDocumentPreflight(context.Background(), htmlFile, pdfFile, options)
	var preflightErr *PreflightError
	if !errors.As(err, &preflightErr) || report.Passed {
		t.Fatalf("expected typed failed preflight, got report=%+v err=%v", report, err)
	}
	for _, name := range []string{"pdf-pages", "pdf-page-size", "pdf-fonts", "pdf-raw-tex"} {
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("failure did not include %s: %v", name, err)
		}
	}
	if _, statErr := os.Stat(pdfFile); statErr != nil {
		t.Fatalf("diagnostic PDF was removed: %v", statErr)
	}
	assertReportFile(t, reportFile, false)
}

func fakePreflightOptions(t *testing.T, root string) PreflightOptions {
	t.Helper()
	return PreflightOptions{
		Enabled:         true,
		ChromiumBinary:  fakeTool(t, root, "chromium", `<!doctype html><meta name="karte-preflight-overflow" content="%5B%5D">`+"\n"),
		PDFInfoBinary:   fakeTool(t, root, "pdfinfo", "Pages: 48\nPage size: 498.96 x 708.96 pts\n"),
		PDFFontsBinary:  fakeTool(t, root, "pdffonts", "AAAAAA+NotoSansJP CID TrueType Identity-H yes yes yes 5 0\n"),
		PDFToTextBinary: fakeTool(t, root, "pdftotext", "Book\n"),
	}
}

func fakeTool(t *testing.T, root, name, output string) string {
	t.Helper()
	path := filepath.Join(root, name)
	writeFile(t, path, "#!/bin/sh\nprintf '%s' "+shellSingleQuote(output)+"\n")
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func requireShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake external tools use POSIX shell scripts")
	}
}

func assertReportFile(t *testing.T, path string, wantPassed bool) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var report PreflightReport
	if err := json.Unmarshal(content, &report); err != nil {
		t.Fatal(err)
	}
	if report.Passed != wantPassed || report.SchemaVersion != 1 {
		t.Fatalf("unexpected persisted report: %+v", report)
	}
}
