package renderer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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
	if runtime.GOOS == "windows" && (strings.Contains(html, `%5C`) || strings.Contains(html, `\\`)) {
		t.Fatalf("base URL contains Windows path separators:\n%s", html)
	}
	assertContains(t, html, `id="karte-renderer-css"`)
	assertContains(t, html, "#c2b4ff")
}

func TestFileURLNormalizesWindowsPath(t *testing.T) {
	got := fileURLForOS(`C:\Users\Karte User\docs\`, "windows")
	want := "file:///C:/Users/Karte%20User/docs/"
	if got != want {
		t.Fatalf("fileURLForOS() = %q, want %q", got, want)
	}
}

func TestFileURLPreservesPOSIXBackslash(t *testing.T) {
	got := fileURLForOS(`/tmp/a\b/docs/`, "linux")
	want := "file:///tmp/a%5Cb/docs/"
	if got != want {
		t.Fatalf("fileURLForOS() = %q, want %q", got, want)
	}
}

func TestFileURLUsesUNCServerAsHost(t *testing.T) {
	got := fileURLForOS(`\\server\share\docs\`, "windows")
	want := "file://server/share/docs/"
	if got != want {
		t.Fatalf("fileURLForOS() = %q, want %q", got, want)
	}
}

func TestFileURLNormalizesExtendedWindowsDrivePath(t *testing.T) {
	got := fileURLForOS(`\\?\C:\docs\`, "windows")
	want := "file:///C:/docs/"
	if got != want {
		t.Fatalf("fileURLForOS() = %q, want %q", got, want)
	}
}

func TestFileURLNormalizesExtendedWindowsUNCPath(t *testing.T) {
	got := fileURLForOS(`\\?\UNC\server\share\docs\`, "windows")
	want := "file://server/share/docs/"
	if got != want {
		t.Fatalf("fileURLForOS() = %q, want %q", got, want)
	}
}

func TestFileURLNormalizesLocalDeviceWindowsDrivePath(t *testing.T) {
	got := fileURLForOS(`\\.\C:\docs\`, "windows")
	want := "file:///C:/docs/"
	if got != want {
		t.Fatalf("fileURLForOS() = %q, want %q", got, want)
	}
}

func TestFileURLNormalizesLocalDeviceWindowsUNCPath(t *testing.T) {
	got := fileURLForOS(`\\.\UNC\server\share\docs\`, "windows")
	want := "file://server/share/docs/"
	if got != want {
		t.Fatalf("fileURLForOS() = %q, want %q", got, want)
	}
}

func TestConvertDocumentPreservesMultilineKatex(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "formula.md")
	output := filepath.Join(root, "formula.html")
	writeFile(t, input, "$$$\n\\begin{aligned}\nx &= 1 \\\\\ny &= 2\n\\end{aligned}\n$$$\n")

	if _, err := ConvertFile(context.Background(), input, output, ConvertOptions{Root: root}); err != nil {
		t.Fatal(err)
	}
	rendered, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, string(rendered), `<div class="katex-display" data-katex="\begin{aligned}`)
	if strings.Contains(string(rendered), `<p><div class="katex-display"`) {
		t.Fatalf("converted HTML contains an invalid paragraph wrapper:\n%s", rendered)
	}
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

func TestConvertMarpChartAutomaticallyEnablesHTML(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell helper is Unix-only")
	}
	root := t.TempDir()
	input := filepath.Join(root, "slides.md")
	output := filepath.Join(root, "slides.html")
	argsFile := filepath.Join(root, "args.txt")
	writeFile(t, filepath.Join(root, "data.csv"), "x,y\n1,2\n2,3\n")
	writeFile(t, input, "---\nmarp: true\n---\n@chart(type=\"line\" path=\"data.csv\" x=\"x\" y=\"y\")\n")
	binary := filepath.Join(root, "fake-marp")
	writeExecutable(t, binary, "#!/bin/sh\nprintf '%s\\n' \"$@\" > "+shellSingleQuote(argsFile)+"\nwhile [ \"$#\" -gt 0 ]; do\n  if [ \"$1\" = \"--output\" ]; then\n    shift\n    printf 'fake-html' > \"$1\"\n    exit 0\n  fi\n  shift\ndone\nexit 2\n")
	if _, err := ConvertFile(context.Background(), input, output, ConvertOptions{Root: root, Marp: MarpOptions{Binary: binary}}); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(args), "--html\n") {
		t.Fatalf("generated chart HTML was sent to Marp without --html:\n%s", args)
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

func TestExportHTMLPDFStopsChromiumAfterCompletedTrailer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell helper is Unix-only")
	}
	root := t.TempDir()
	htmlFile := filepath.Join(root, "page.html")
	output := filepath.Join(root, "page.pdf")
	writeFile(t, htmlFile, "<!doctype html><title>test</title>")
	binary := filepath.Join(root, "hanging-chromium")
	writeExecutable(t, binary, `#!/bin/sh
for arg in "$@"; do
  case "$arg" in
    --print-to-pdf=*)
      output=${arg#*=}
      printf '%%PDF-1.4\n1 0 obj\n<<>>\nendobj\n%%%%EOF\n' > "$output"
      exec sleep 60
      ;;
  esac
done
exit 2
`)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	started := time.Now()
	if err := ExportHTMLPDF(ctx, htmlFile, output, PDFOptions{Engine: "chromium", Binary: binary}); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) >= 2*time.Second {
		t.Fatalf("completed Chromium PDF did not return promptly")
	}
	if !completedPDF(output) {
		t.Fatal("completed PDF trailer was not retained")
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

func TestFindChromiumForWindowsPrefersPath(t *testing.T) {
	wanted := filepath.Join("path", "msedge.exe")
	got := findChromiumForOS(
		"windows",
		func(string) string { return filepath.Join(`C:\`, "Program Files") },
		func(name string) (string, error) {
			if name == "msedge.exe" {
				return wanted, nil
			}
			return "", errors.New("not found")
		},
		func(string) bool { return true },
	)
	if got != wanted {
		t.Fatalf("findChromiumForOS() = %q, want PATH result %q", got, wanted)
	}
}

func TestFindChromiumForWindowsUsesStandardInstallLocations(t *testing.T) {
	programFilesX86 := filepath.Join(`C:\`, "Program Files (x86)")
	programFiles := filepath.Join(`C:\`, "Program Files")
	localAppData := filepath.Join(`C:\`, "Users", "karte", "AppData", "Local")
	wanted := filepath.Join(programFilesX86, "Microsoft", "Edge", "Application", "msedge.exe")

	got := findChromiumForOS(
		"windows",
		func(name string) string {
			switch name {
			case "ProgramFiles(x86)":
				return programFilesX86
			case "ProgramFiles":
				return programFiles
			case "LOCALAPPDATA":
				return localAppData
			default:
				return ""
			}
		},
		func(string) (string, error) { return "", errors.New("not found") },
		func(path string) bool { return path == wanted },
	)
	if got != wanted {
		t.Fatalf("findChromiumForOS() = %q, want installed Edge %q", got, wanted)
	}
}

func TestFindChromiumForWindowsFallsBackToPerUserChrome(t *testing.T) {
	localAppData := filepath.Join(`C:\`, "Users", "karte", "AppData", "Local")
	wanted := filepath.Join(localAppData, "Google", "Chrome", "Application", "chrome.exe")

	got := findChromiumForOS(
		"windows",
		func(name string) string {
			if name == "LOCALAPPDATA" {
				return localAppData
			}
			return ""
		},
		func(string) (string, error) { return "", errors.New("not found") },
		func(path string) bool { return path == wanted },
	)
	if got != wanted {
		t.Fatalf("findChromiumForOS() = %q, want per-user Chrome %q", got, wanted)
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
