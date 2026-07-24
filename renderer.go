package renderer

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"html"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	"gopkg.in/yaml.v3"
)

// FrontMatter contains YAML metadata found at the top of a Markdown document.
type FrontMatter struct {
	Title   string                 `yaml:"title"`
	Marp    bool                   `yaml:"marp"`
	Theme   string                 `yaml:"theme"`
	Layout  string                 `yaml:"layout"`
	Owners  []string               `yaml:"owners"`
	Viewers []string               `yaml:"viewers"`
	Data    map[string]interface{} `yaml:",inline"`
}

// FileSystem abstracts file access for renderers and tests.
type FileSystem interface {
	ReadFile(name string) ([]byte, error)
	Stat(name string) (fs.FileInfo, error)
	Open(name string) (io.ReadCloser, error)
}

// OSFileSystem implements FileSystem using the os package.
type OSFileSystem struct{}

func (OSFileSystem) ReadFile(name string) ([]byte, error)    { return os.ReadFile(name) }
func (OSFileSystem) Stat(name string) (fs.FileInfo, error)   { return os.Stat(name) }
func (OSFileSystem) Open(name string) (io.ReadCloser, error) { return os.Open(name) }

// Renderer bundles Karte-compatible Markdown, Marp, and PDF rendering helpers.
type Renderer struct{ fs FileSystem }

// NewRenderer constructs a Renderer.
func NewRenderer(fs FileSystem) *Renderer {
	if fs == nil {
		fs = OSFileSystem{}
	}
	return &Renderer{fs: fs}
}

const fallbackLayout = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{TITLE}}</title>
{{CSS}}
</head>
<body>{{CONTENT}}</body>
</html>`

var defaultRenderer = NewRenderer(OSFileSystem{})
var fmRe = regexp.MustCompile(`(?s)\A---[ \t]*\r?\n(.*?)\r?\n---[ \t]*(?:\r?\n|\z)`)
var importRe = regexp.MustCompile(`(?m)^@import\(([^)]*)\)[ \t]*$`)

// RenderMarkdown renders a Markdown file below root, returning HTML and front matter.
func RenderMarkdown(root string, path string) (string, FrontMatter, error) {
	return defaultRenderer.RenderMarkdownWithOptions(root, path, false)
}

// RenderMarkdownWithOptions renders a Markdown file with options such as hard wrapping.
func RenderMarkdownWithOptions(root, path string, hardwrap bool) (string, FrontMatter, error) {
	return defaultRenderer.RenderMarkdownWithOptions(root, path, hardwrap)
}

// RenderString renders a Markdown string, returning HTML and front matter.
func RenderString(root string, markdown string) (string, FrontMatter, error) {
	return defaultRenderer.RenderStringWithOptions(root, markdown, false)
}

// RenderStringWithOptions renders Markdown from a string with options.
func RenderStringWithOptions(root, markdown string, hardwrap bool) (string, FrontMatter, error) {
	return defaultRenderer.RenderStringWithOptions(root, markdown, hardwrap)
}

func (r *Renderer) RenderMarkdownWithOptions(root, path string, hardwrap bool) (string, FrontMatter, error) {
	return r.renderMarkdownWithCSS(root, path, hardwrap, defaultDocumentCSS)
}

func (r *Renderer) renderMarkdownWithCSS(root, path string, hardwrap bool, css string) (string, FrontMatter, error) {
	root, err := r.normalizeRoot(root)
	if err != nil {
		return "", FrontMatter{}, err
	}
	full, err := safeJoin(root, path)
	if err != nil {
		return "", FrontMatter{}, err
	}
	if r.usesOSFileSystem() {
		full, err = resolveWithinRoot(root, full)
		if err != nil {
			return "", FrontMatter{}, err
		}
	}
	b, err := r.fs.ReadFile(full)
	if err != nil {
		return "", FrontMatter{}, err
	}
	return r.render(root, filepath.Dir(full), string(b), hardwrap, css)
}

func (r *Renderer) RenderStringWithOptions(root, markdown string, hardwrap bool) (string, FrontMatter, error) {
	var err error
	root, err = r.normalizeRoot(root)
	if err != nil {
		return "", FrontMatter{}, err
	}
	return r.render(root, root, markdown, hardwrap, defaultDocumentCSS)
}

func (r *Renderer) render(root, baseDir, markdown string, hardwrap bool, css string) (string, FrontMatter, error) {
	body, fm, err := parseFrontMatter(markdown)
	if err != nil {
		return "", fm, err
	}
	body, err = r.expandImports(root, baseDir, body, hardwrap)
	if err != nil {
		return "", fm, err
	}
	var content string
	if fm.Marp {
		content, err = RenderMarp(body)
	} else {
		content, err = markdownHTML(body, hardwrap)
	}
	if err != nil {
		return "", fm, err
	}
	layout, err := r.loadLayout(root)
	if err != nil {
		return "", fm, err
	}
	out := strings.ReplaceAll(layout, "{{TITLE}}", html.EscapeString(fm.Title))
	out = strings.ReplaceAll(out, "{{CSS}}", documentStyle(css))
	out = strings.ReplaceAll(out, "{{CONTENT}}", content)
	return out, fm, nil
}

func parseFrontMatter(s string) (string, FrontMatter, error) {
	fm := FrontMatter{Data: map[string]interface{}{}}
	m := fmRe.FindStringSubmatchIndex(s)
	if m == nil {
		return s, fm, nil
	}
	if err := parseYAML(s[m[2]:m[3]], &fm); err != nil {
		return "", fm, err
	}
	return s[m[1]:], fm, nil
}

func parseYAML(yml string, fm *FrontMatter) error {
	type knownFrontMatter struct {
		Title  string `yaml:"title"`
		Marp   bool   `yaml:"marp"`
		Theme  string `yaml:"theme"`
		Layout string `yaml:"layout"`
	}
	var known knownFrontMatter
	if err := yaml.Unmarshal([]byte(yml), &known); err != nil {
		return fmt.Errorf("invalid YAML front matter: %w", err)
	}
	data := map[string]interface{}{}
	if err := yaml.Unmarshal([]byte(yml), &data); err != nil {
		return fmt.Errorf("invalid YAML front matter: %w", err)
	}
	fm.Title = known.Title
	fm.Marp = known.Marp
	fm.Theme = known.Theme
	fm.Layout = known.Layout
	owners, err := frontMatterStringList(data["owners"])
	if err != nil {
		return fmt.Errorf("invalid YAML front matter owners: %w", err)
	}
	viewers, err := frontMatterStringList(data["viewers"])
	if err != nil {
		return fmt.Errorf("invalid YAML front matter viewers: %w", err)
	}
	fm.Owners = owners
	fm.Viewers = viewers
	fm.Data = data
	return nil
}

func frontMatterStringList(value interface{}) ([]string, error) {
	switch value := value.(type) {
	case nil:
		return nil, nil
	case string:
		return parseCSVList(value), nil
	case []interface{}:
		out := make([]string, 0, len(value))
		for _, item := range value {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("expected string list, got %T", item)
			}
			out = append(out, text)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("expected string or string list, got %T", value)
	}
}

func markdownHTML(s string, hardwrap bool) (string, error) {
	rendererOptions := []goldmark.Option{
		goldmark.WithExtensions(
			extension.GFM,
			extension.Footnote,
			extension.DefinitionList,
		),
		goldmark.WithRendererOptions(gmhtml.WithUnsafe()),
	}
	if hardwrap {
		rendererOptions = append(rendererOptions, goldmark.WithRendererOptions(gmhtml.WithHardWraps()))
	}
	md := goldmark.New(rendererOptions...)
	var out bytes.Buffer
	if err := md.Convert([]byte(s), &out); err != nil {
		return "", fmt.Errorf("render markdown: %w", err)
	}
	return processKaTeX(out.String()), nil
}

func (r *Renderer) expandImports(root, baseDir, s string, hardwrap bool) (string, error) {
	return r.expandImportsRecursive(root, baseDir, s, hardwrap, map[string]bool{}, 0)
}

func (r *Renderer) expandImportsRecursive(root, baseDir, s string, hardwrap bool, stack map[string]bool, depth int) (string, error) {
	if depth > 32 {
		return "", fmt.Errorf("maximum @import depth exceeded")
	}
	var firstErr error
	out := importRe.ReplaceAllStringFunc(s, func(m string) string {
		if firstErr != nil {
			return ""
		}
		attrs := parseAttrs(importRe.FindStringSubmatch(m)[1])
		typ, p := attrs["type"], attrs["path"]
		if p == "" {
			p = attrs["src"]
		}
		if p == "" {
			firstErr = fmt.Errorf("@import missing path/src")
			return ""
		}
		full := p
		if !filepath.IsAbs(p) {
			full = filepath.Join(baseDir, p)
		}
		full = filepath.Clean(full)
		if !isWithin(root, full) {
			firstErr = fmt.Errorf("import path escapes root: %s", p)
			return ""
		}
		if r.usesOSFileSystem() {
			if resolved, err := resolveWithinRoot(root, full); err != nil {
				firstErr = err
				return ""
			} else {
				full = resolved
			}
		}
		if stack[full] {
			firstErr = fmt.Errorf("cyclic @import detected at %s", p)
			return ""
		}
		b, err := r.fs.ReadFile(full)
		if err != nil {
			firstErr = err
			return ""
		}
		switch typ {
		case "csv":
			h, err := csvToHTML(b, attrs)
			if err != nil {
				firstErr = err
				return ""
			}
			return h
		case "tex":
			h, err := texToHTML(b, attrs)
			if err != nil {
				firstErr = err
				return ""
			}
			return h
		case "md", "markdown":
			stack[full] = true
			nested, err := r.expandImportsRecursive(root, filepath.Dir(full), string(b), hardwrap, stack, depth+1)
			delete(stack, full)
			if err != nil {
				firstErr = err
				return ""
			}
			return nested
		default:
			firstErr = fmt.Errorf("unsupported import type %q", typ)
			return ""
		}
	})
	return out, firstErr
}

func parseAttrs(s string) map[string]string {
	attrs := map[string]string{}
	re := regexp.MustCompile(`(\w+)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s]+))`)
	for _, m := range re.FindAllStringSubmatch(s, -1) {
		v := m[2]
		if v == "" {
			v = m[3]
		}
		if v == "" {
			v = m[4]
		}
		attrs[m[1]] = v
	}
	return attrs
}

func csvToHTML(b []byte, attrs map[string]string) (string, error) {
	recs, err := csv.NewReader(bytes.NewReader(b)).ReadAll()
	if err != nil {
		return "", err
	}
	cols := csvColumns(recs, attrs["select"])
	var sb strings.Builder
	sb.WriteString("<table>\n")
	for i, r := range recs {
		if i == 0 {
			sb.WriteString("<thead>\n<tr>")
		} else if i == 1 {
			sb.WriteString("<tbody>\n<tr>")
		} else {
			sb.WriteString("<tr>")
		}
		cell := "td"
		if i == 0 {
			cell = "th"
		}
		for _, c := range cols {
			val := ""
			if c < len(r) {
				val = r[c]
			}
			fmt.Fprintf(&sb, "<%s>%s</%s>", cell, html.EscapeString(val), cell)
		}
		if i == 0 {
			sb.WriteString("</tr>\n</thead>\n")
		} else {
			sb.WriteString("</tr>\n")
		}
	}
	if len(recs) > 1 {
		sb.WriteString("</tbody>\n")
	}
	sb.WriteString("</table>")
	return sb.String(), nil
}

func texToHTML(b []byte, attrs map[string]string) (string, error) {
	expr := strings.TrimSpace(string(b))
	if expr == "" {
		return "", fmt.Errorf("empty TeX import")
	}
	escaped := html.EscapeString(expr)
	switch strings.ToLower(strings.TrimSpace(attrs["display"])) {
	case "", "block", "true":
		return `<div class="katex-display" data-katex="` + escaped + `">` + escaped + `</div>`, nil
	case "inline", "false":
		return `<span class="katex" data-katex="` + escaped + `">` + escaped + `</span>`, nil
	default:
		return "", fmt.Errorf("invalid TeX display mode %q (use block or inline)", attrs["display"])
	}
}

func parseCSVList(s string) []string {
	s = strings.TrimSpace(strings.Trim(s, "[]"))
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	for i := range parts {
		parts[i] = strings.Trim(strings.TrimSpace(parts[i]), "\"'")
	}
	return parts
}

func csvColumns(recs [][]string, selectList string) []int {
	if len(recs) == 0 {
		return nil
	}
	if strings.TrimSpace(selectList) == "" {
		out := make([]int, len(recs[0]))
		for i := range out {
			out[i] = i
		}
		return out
	}
	want := strings.Split(selectList, ",")
	var out []int
	for _, name := range want {
		name = strings.TrimSpace(name)
		for i, h := range recs[0] {
			if h == name {
				out = append(out, i)
				break
			}
		}
	}
	return out
}

// ParseSlides parses Marp markdown content into individual slides.
func ParseSlides(content string) []string {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	parts := regexp.MustCompile(`(?m)^---\s*$`).Split(content, -1)
	slides := []string{}
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			slides = append(slides, t)
		}
	}
	return slides
}

// RenderMarp renders Marp slides into section-based HTML.
func RenderMarp(content string) (string, error) {
	var sb strings.Builder
	for _, slide := range ParseSlides(content) {
		inner, err := markdownHTML(stripSlideDirectives(slide), false)
		if err != nil {
			return "", err
		}
		classes := append([]string{"marp-slide"}, slideClasses(slide)...)
		fmt.Fprintf(&sb, `<section class="%s">`, html.EscapeString(strings.Join(classes, " ")))
		sb.WriteString(inner)
		sb.WriteString("</section>\n")
	}
	return sb.String(), nil
}

func stripSlideDirectives(s string) string {
	lines := strings.Split(s, "\n")
	out := lines[:0]
	for _, l := range lines {
		if strings.Contains(strings.TrimSpace(l), "_class:") && strings.HasPrefix(strings.TrimSpace(l), "<!--") {
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}
func slideClasses(s string) []string {
	re := regexp.MustCompile(`_class:\s*([^->]+)`)
	m := re.FindStringSubmatch(s)
	if len(m) < 2 {
		return nil
	}
	return strings.Fields(strings.TrimSpace(m[1]))
}

func processKaTeX(s string) string {
	type ph struct{ key, val string }
	var saved []ph
	protect := func(re *regexp.Regexp, in string) string {
		return re.ReplaceAllStringFunc(in, func(m string) string {
			k := fmt.Sprintf("\x00K%d\x00", len(saved))
			saved = append(saved, ph{k, m})
			return k
		})
	}
	s = protect(regexp.MustCompile(`(?s)<pre[^>]*>.*?</pre>`), s)
	s = protect(regexp.MustCompile(`(?s)<code[^>]*>.*?</code>`), s)
	s = regexp.MustCompile(`(?s)\$\$\$(.+?)\$\$\$`).ReplaceAllStringFunc(s, func(m string) string {
		expr := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(m, "$$$"), "$$$"))
		expr = html.UnescapeString(expr)
		return `<div class="katex-display" data-katex="` + html.EscapeString(expr) + `">` + html.EscapeString(expr) + `</div>`
	})
	s = regexp.MustCompile(`\$([^$\n]+?)\$`).ReplaceAllStringFunc(s, func(m string) string {
		expr := strings.TrimSuffix(strings.TrimPrefix(m, "$"), "$")
		expr = html.UnescapeString(expr)
		return `<span class="katex" data-katex="` + html.EscapeString(expr) + `">` + html.EscapeString(expr) + `</span>`
	})
	for _, p := range saved {
		s = strings.ReplaceAll(s, p.key, p.val)
	}
	return s
}

// ExportPDF writes HTML to a PDF using Chromium when available, with
// wkhtmltopdf as a compatibility fallback.
func ExportPDF(htmlFile, outputPDF string) error {
	return ExportHTMLPDF(nil, htmlFile, outputPDF, PDFOptions{AllowLocalFiles: true})
}

// ExportPDFWithBinary writes HTML to a PDF with an explicit binary or PATH/default lookup.
func ExportPDFWithBinary(binary, htmlFile, outputPDF string) error {
	if binary == "" {
		binary = findWkhtmltopdf()
	}
	if binary == "" {
		return fmt.Errorf("wkhtmltopdf not found")
	}
	cmd := exec.Command(binary, "--enable-local-file-access", htmlFile, outputPDF)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("wkhtmltopdf failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func findWkhtmltopdf() string {
	if p, err := exec.LookPath("wkhtmltopdf"); err == nil {
		return p
	}
	for _, p := range []string{`C:\Program Files\wkhtmltopdf\bin\wkhtmltopdf.exe`, `/usr/local/bin/wkhtmltopdf`, `/opt/homebrew/bin/wkhtmltopdf`} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func (r *Renderer) loadLayout(root string) (string, error) {
	for _, name := range []string{"preview.html", "layout.html"} {
		p := filepath.Join(root, "themes", "default", name)
		b, err := r.fs.ReadFile(p)
		if err == nil {
			return string(b), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
	}
	return fallbackLayout, nil
}
func safeJoin(root, p string) (string, error) {
	full := p
	if !filepath.IsAbs(p) {
		full = filepath.Join(root, p)
	}
	full = filepath.Clean(full)
	if !isWithin(root, full) {
		return "", fmt.Errorf("path escapes root: %s", p)
	}
	return full, nil
}
func isWithin(root, full string) bool {
	root, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	full, err = filepath.Abs(full)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, full)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func resolveWithinRoot(root, full string) (string, error) {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}
	resolvedFull, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "", err
	}
	if !isWithin(resolvedRoot, resolvedFull) {
		return "", fmt.Errorf("path escapes root through symlink: %s", full)
	}
	return resolvedFull, nil
}

func canonicalRoot(root string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}
	return root, nil
}

func (r *Renderer) usesOSFileSystem() bool {
	switch r.fs.(type) {
	case OSFileSystem, *OSFileSystem:
		return true
	default:
		return false
	}
}

func (r *Renderer) normalizeRoot(root string) (string, error) {
	if r.usesOSFileSystem() {
		return canonicalRoot(root)
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}
	return root, nil
}
