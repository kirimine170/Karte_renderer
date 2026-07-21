package renderer

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ConvertOptions configures file-to-file conversion.
type ConvertOptions struct {
	// Root is the trusted project root used for themes and @import. It defaults
	// to the input file's directory.
	Root     string
	HardWrap bool
	// CSS replaces the built-in document stylesheet. Relative paths are
	// resolved from the current working directory.
	CSS string
	// NoCSS disables stylesheet injection for normal Markdown documents.
	NoCSS bool
	Marp  MarpOptions
	PDF   PDFOptions
}

// MarpOptions configures conversions delegated to the official Marp CLI.
type MarpOptions struct {
	Binary          string
	Theme           string
	ThemeSet        []string
	HTML            bool
	AllowLocalFiles bool
	BrowserPath     string
	EditablePPTX    bool
	ExtraArgs       []string
}

// PDFOptions configures printing a normal HTML document to PDF.
type PDFOptions struct {
	// Engine may be "auto", "chromium", or "wkhtmltopdf".
	Engine          string
	Binary          string
	AllowLocalFiles bool
	ExtraArgs       []string
}

// DependencyStatus describes an optional external rendering dependency.
type DependencyStatus struct {
	Name        string
	Path        string
	Found       bool
	RequiredFor string
}

// ConvertFile converts Markdown to HTML or PDF, and Marp Markdown to HTML,
// PDF, or PPTX. Marp conversion is selected by `marp: true` front matter or a
// .pptx destination.
func ConvertFile(ctx context.Context, input, output string, opts ConvertOptions) (FrontMatter, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	inputAbs, err := filepath.Abs(input)
	if err != nil {
		return FrontMatter{}, fmt.Errorf("resolve input: %w", err)
	}
	outputAbs, err := filepath.Abs(output)
	if err != nil {
		return FrontMatter{}, fmt.Errorf("resolve output: %w", err)
	}
	if inputAbs == outputAbs {
		return FrontMatter{}, fmt.Errorf("input and output must be different files")
	}
	source, err := os.ReadFile(inputAbs)
	if err != nil {
		return FrontMatter{}, fmt.Errorf("read input: %w", err)
	}
	_, fm, err := parseFrontMatter(string(source))
	if err != nil {
		return fm, err
	}
	root := opts.Root
	if root == "" {
		root = filepath.Dir(inputAbs)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return fm, fmt.Errorf("resolve project root: %w", err)
	}
	if !isWithin(root, inputAbs) {
		return fm, fmt.Errorf("input %s is outside project root %s", inputAbs, root)
	}
	if err := os.MkdirAll(filepath.Dir(outputAbs), 0o755); err != nil {
		return fm, fmt.Errorf("create output directory: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(outputAbs))
	isMarp := fm.Marp || ext == ".pptx"
	if isMarp {
		if ext != ".html" && ext != ".htm" && ext != ".pdf" && ext != ".pptx" {
			return fm, fmt.Errorf("unsupported Marp output extension %q (use .html, .pdf, or .pptx)", ext)
		}
		marpInput, cleanup, err := prepareMarpInput(root, inputAbs, string(source), opts.HardWrap)
		if err != nil {
			return fm, err
		}
		defer cleanup()
		if err := ExportMarp(ctx, marpInput, outputAbs, opts.Marp); err != nil {
			return fm, err
		}
		return fm, nil
	}

	if ext != ".html" && ext != ".htm" && ext != ".pdf" {
		return fm, fmt.Errorf("unsupported document output extension %q (use .html or .pdf)", ext)
	}
	rel, err := filepath.Rel(root, inputAbs)
	if err != nil {
		return fm, fmt.Errorf("locate input below root: %w", err)
	}
	css, err := loadDocumentCSS(opts)
	if err != nil {
		return fm, err
	}
	rendered, renderedFM, err := defaultRenderer.renderMarkdownWithCSS(root, rel, opts.HardWrap, css)
	if err != nil {
		return fm, err
	}
	rendered = insertBaseURL(rendered, filepath.Dir(inputAbs))
	if ext == ".html" || ext == ".htm" {
		if err := writeFileAtomic(outputAbs, []byte(rendered)); err != nil {
			return renderedFM, err
		}
		return renderedFM, nil
	}

	tmp, err := os.CreateTemp("", "karte-renderer-*.html")
	if err != nil {
		return renderedFM, fmt.Errorf("create temporary HTML: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.WriteString(rendered); err != nil {
		tmp.Close()
		return renderedFM, fmt.Errorf("write temporary HTML: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return renderedFM, fmt.Errorf("close temporary HTML: %w", err)
	}
	pdfOpts := opts.PDF
	pdfOpts.AllowLocalFiles = true // the generated document uses a root-scoped file: base URL
	if err := ExportHTMLPDF(ctx, tmpName, outputAbs, pdfOpts); err != nil {
		return renderedFM, err
	}
	return renderedFM, nil
}

func loadDocumentCSS(opts ConvertOptions) (string, error) {
	if opts.NoCSS && opts.CSS != "" {
		return "", fmt.Errorf("--css and --no-css cannot be used together")
	}
	if opts.NoCSS {
		return "", nil
	}
	if opts.CSS == "" {
		return defaultDocumentCSS, nil
	}
	b, err := os.ReadFile(opts.CSS)
	if err != nil {
		return "", fmt.Errorf("read document CSS: %w", err)
	}
	return string(b), nil
}

func prepareMarpInput(root, input, source string, hardwrap bool) (string, func(), error) {
	if !importRe.MatchString(source) {
		return input, func() {}, nil
	}
	r := NewRenderer(OSFileSystem{})
	expanded, err := r.expandImports(root, filepath.Dir(input), source, hardwrap)
	if err != nil {
		return "", func() {}, fmt.Errorf("expand Marp imports: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(input), ".karte-marp-*.md")
	if err != nil {
		return "", func() {}, fmt.Errorf("create temporary Marp input: %w", err)
	}
	name := tmp.Name()
	cleanup := func() { _ = os.Remove(name) }
	if _, err := tmp.WriteString(expanded); err != nil {
		tmp.Close()
		cleanup()
		return "", func() {}, fmt.Errorf("write temporary Marp input: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("close temporary Marp input: %w", err)
	}
	return name, cleanup, nil
}

// ExportMarp converts a Markdown slide deck with the official Marp CLI.
func ExportMarp(ctx context.Context, input, output string, opts MarpOptions) error {
	binary := opts.Binary
	if binary == "" {
		binary = os.Getenv("MARP_BINARY")
	}
	if binary == "" {
		binary = findMarpBinary(filepath.Dir(input))
	}
	if binary == "" {
		return fmt.Errorf("Marp CLI not found; run npm install or set MARP_BINARY")
	}
	inputAbs, err := filepath.Abs(input)
	if err != nil {
		return fmt.Errorf("resolve Marp input: %w", err)
	}
	outputAbs, err := filepath.Abs(output)
	if err != nil {
		return fmt.Errorf("resolve Marp output: %w", err)
	}
	args := []string{inputAbs, "--output", outputAbs}
	if opts.Theme != "" {
		args = append(args, "--theme", opts.Theme)
	}
	for _, themeSet := range opts.ThemeSet {
		args = append(args, "--theme-set", themeSet)
	}
	if opts.AllowLocalFiles {
		args = append(args, "--allow-local-files")
	}
	if opts.HTML {
		args = append(args, "--html")
	}
	if opts.BrowserPath != "" {
		args = append(args, "--browser-path", opts.BrowserPath)
	}
	if opts.EditablePPTX {
		if strings.ToLower(filepath.Ext(outputAbs)) != ".pptx" {
			return fmt.Errorf("editable PPTX is only valid for .pptx output")
		}
		args = append(args, "--pptx-editable")
	}
	args = append(args, opts.ExtraArgs...)
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = filepath.Dir(inputAbs)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("Marp conversion failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if _, err := os.Stat(outputAbs); err != nil {
		return fmt.Errorf("Marp did not create output %s: %w", outputAbs, err)
	}
	return nil
}

// ExportHTMLPDF prints a standalone HTML document to PDF. Auto mode prefers a
// Chromium-family browser and falls back to wkhtmltopdf.
func ExportHTMLPDF(ctx context.Context, htmlFile, outputPDF string, opts PDFOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	engine := strings.ToLower(opts.Engine)
	if engine == "" {
		engine = "auto"
	}
	if engine != "auto" && engine != "chromium" && engine != "wkhtmltopdf" {
		return fmt.Errorf("unknown PDF engine %q", opts.Engine)
	}
	binary := opts.Binary
	if binary == "" {
		binary = os.Getenv("KARTE_PDF_BINARY")
	}
	if binary != "" && engine == "auto" {
		if strings.Contains(strings.ToLower(filepath.Base(binary)), "wkhtmltopdf") {
			engine = "wkhtmltopdf"
		} else {
			engine = "chromium"
		}
	}
	if binary == "" && (engine == "auto" || engine == "chromium") {
		binary = findChromium()
		if binary != "" {
			engine = "chromium"
		}
	}
	if binary == "" && (engine == "auto" || engine == "wkhtmltopdf") {
		binary = findWkhtmltopdf()
		if binary != "" {
			engine = "wkhtmltopdf"
		}
	}
	if binary == "" {
		return fmt.Errorf("no PDF engine found; install Chrome/Chromium or wkhtmltopdf, or set KARTE_PDF_BINARY")
	}

	htmlAbs, err := filepath.Abs(htmlFile)
	if err != nil {
		return fmt.Errorf("resolve HTML input: %w", err)
	}
	outputAbs, err := filepath.Abs(outputPDF)
	if err != nil {
		return fmt.Errorf("resolve PDF output: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(outputAbs), 0o755); err != nil {
		return fmt.Errorf("create PDF directory: %w", err)
	}
	var args []string
	var cleanup func()
	if engine == "chromium" {
		profile, err := os.MkdirTemp("", "karte-chromium-*")
		if err != nil {
			return fmt.Errorf("create Chromium profile: %w", err)
		}
		cleanup = func() { _ = os.RemoveAll(profile) }
		args = []string{
			"--headless",
			"--disable-gpu",
			"--disable-extensions",
			"--no-pdf-header-footer",
			"--print-to-pdf-no-header",
			"--user-data-dir=" + profile,
			"--print-to-pdf=" + outputAbs,
		}
		if opts.AllowLocalFiles {
			args = append(args, "--allow-file-access-from-files")
		}
		args = append(args, opts.ExtraArgs...)
		args = append(args, (&url.URL{Scheme: "file", Path: htmlAbs}).String())
	} else {
		cleanup = func() {}
		if opts.AllowLocalFiles {
			args = append(args, "--enable-local-file-access")
		}
		args = append(args, opts.ExtraArgs...)
		args = append(args, htmlAbs, outputAbs)
	}
	defer cleanup()
	cmd := exec.CommandContext(ctx, binary, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s PDF conversion failed: %w: %s", engine, err, strings.TrimSpace(string(out)))
	}
	if _, err := os.Stat(outputAbs); err != nil {
		return fmt.Errorf("PDF engine did not create output %s: %w", outputAbs, err)
	}
	return nil
}

// Diagnose reports the optional tools available to the conversion pipeline.
func Diagnose(searchFrom string) []DependencyStatus {
	marp := findMarpBinary(searchFrom)
	chromium := findChromium()
	wkhtml := findWkhtmltopdf()
	return []DependencyStatus{
		{Name: "marp", Path: marp, Found: marp != "", RequiredFor: "Marp HTML/PDF/PPTX"},
		{Name: "chromium", Path: chromium, Found: chromium != "", RequiredFor: "PDF/PPTX rendering"},
		{Name: "wkhtmltopdf", Path: wkhtml, Found: wkhtml != "", RequiredFor: "optional document PDF fallback"},
	}
}

func findMarpBinary(start string) string {
	if p, err := exec.LookPath("marp"); err == nil {
		return p
	}
	if start == "" {
		start, _ = os.Getwd()
	}
	start, _ = filepath.Abs(start)
	for dir := start; dir != ""; dir = filepath.Dir(dir) {
		for _, name := range []string{"marp", "marp.cmd"} {
			candidate := filepath.Join(dir, "node_modules", ".bin", name)
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	return ""
}

func findChromium() string {
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "microsoft-edge", "brave-browser"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	if runtime.GOOS == "darwin" {
		for _, p := range []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
		} {
			if info, err := os.Stat(p); err == nil && !info.IsDir() {
				return p
			}
		}
	}
	return ""
}

func insertBaseURL(document, dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return document
	}
	if !strings.HasSuffix(abs, string(os.PathSeparator)) {
		abs += string(os.PathSeparator)
	}
	tag := `<base href="` + (&url.URL{Scheme: "file", Path: abs}).String() + `">`
	if i := strings.Index(strings.ToLower(document), "<head>"); i >= 0 {
		at := i + len("<head>")
		return document[:at] + tag + document[at:]
	}
	return tag + document
}

func writeFileAtomic(path string, content []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".karte-output-*")
	if err != nil {
		return fmt.Errorf("create temporary output: %w", err)
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return fmt.Errorf("write output: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return fmt.Errorf("set output permissions: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close output: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("replace output: %w", err)
	}
	return nil
}
