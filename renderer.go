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

// UsesOSPaths reports that paths can be validated with the host OS. The method
// is promoted by wrappers embedding OSFileSystem, preserving boundary checks.
func (OSFileSystem) UsesOSPaths() bool { return true }

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
{{KATEX}}
</head>
<body>{{CONTENT}}</body>
</html>`

var defaultRenderer = NewRenderer(OSFileSystem{})
var fmRe = regexp.MustCompile(`(?s)\A---[ \t]*\r?\n(.*?)\r?\n---[ \t]*(?:\r?\n|\z)`)
var importRe = regexp.MustCompile(`(?m)^@import\(([^)]*)\)[ \t]*\r?$`)
var rawHTMLCodeOpenRe = regexp.MustCompile(`(?i)<(pre|code)(?:\s[^>]*)?>`)
var katexDisplayRe = regexp.MustCompile(`(?s)\$\$\$(.+?)\$\$\$`)
var katexInlineRe = regexp.MustCompile(`\$([^$\n]+?)\$`)
var katexProtectedCommentRe = regexp.MustCompile(`(?s)<!--.*?-->`)
var katexProtectedPreRe = regexp.MustCompile(`(?s)<pre[^>]*>.*?</pre>`)
var katexProtectedCodeRe = regexp.MustCompile(`(?s)<code[^>]*>.*?</code>`)

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

func (r *Renderer) renderMarkdownResultWithCSS(root, path string, hardwrap bool, css string) (RenderResult, error) {
	root, err := r.normalizeRoot(root)
	if err != nil {
		return RenderResult{}, err
	}
	full, err := safeJoin(root, path)
	if err != nil {
		return RenderResult{}, err
	}
	if r.usesOSFileSystem() {
		full, err = resolveWithinRoot(root, full)
		if err != nil {
			return RenderResult{}, err
		}
	}
	b, err := r.fs.ReadFile(full)
	if err != nil {
		return RenderResult{}, err
	}
	collector := newMetadataCollector(root, r.fs, r.usesOSFileSystem())
	collector.addDependency(DependencySource, full)
	return r.renderResult(root, filepath.Dir(full), string(b), hardwrap, css, collector)
}

func (r *Renderer) RenderStringWithOptions(root, markdown string, hardwrap bool) (string, FrontMatter, error) {
	var err error
	root, err = r.normalizeRoot(root)
	if err != nil {
		return "", FrontMatter{}, err
	}
	return r.render(root, root, markdown, hardwrap, defaultDocumentCSS)
}

// RenderStringResultWithOptions renders Markdown and returns structured render metadata.
func (r *Renderer) RenderStringResultWithOptions(root, markdown string, hardwrap bool) (RenderResult, error) {
	var err error
	root, err = r.normalizeRoot(root)
	if err != nil {
		return RenderResult{}, err
	}
	collector := newMetadataCollector(root, r.fs, r.usesOSFileSystem())
	return r.renderResult(root, root, markdown, hardwrap, defaultDocumentCSS, collector)
}

func (r *Renderer) render(root, baseDir, markdown string, hardwrap bool, css string) (string, FrontMatter, error) {
	result, err := r.renderResult(root, baseDir, markdown, hardwrap, css, nil)
	return result.HTML, result.Metadata.FrontMatter, err
}

func (r *Renderer) renderResult(root, baseDir, markdown string, hardwrap bool, css string, collector *metadataCollector) (RenderResult, error) {
	result := RenderResult{Metadata: RenderMetadata{SchemaVersion: RenderMetadataSchemaVersion}}
	body, fm, err := parseFrontMatter(markdown)
	result.Metadata.FrontMatter = fm
	if err != nil {
		return result, err
	}
	body, err = r.expandImportsWithMetadata(root, baseDir, body, hardwrap, collector)
	if err != nil {
		return result, err
	}
	if collector != nil {
		if fm.Marp {
			for _, slide := range ParseSlides(body) {
				collector.collectReferences(baseDir, stripSlideDirectives(slide))
			}
		} else {
			collector.collectReferences(baseDir, body)
		}
	}
	var content string
	if fm.Marp {
		content, err = RenderMarp(body)
	} else {
		content, err = markdownHTML(body, hardwrap)
	}
	if err != nil {
		return result, err
	}
	layout, layoutPath, err := r.loadLayoutWithPath(root)
	if err != nil {
		return result, err
	}
	if collector != nil && layoutPath != "" {
		collector.addDependency(DependencyLayout, layoutPath)
	}
	runtime := katexRuntimeFor(content)
	hasKaTeXPlaceholder := strings.Contains(layout, "{{KATEX}}")
	out := strings.ReplaceAll(layout, "{{TITLE}}", html.EscapeString(fm.Title))
	out = strings.ReplaceAll(out, "{{CSS}}", documentStyle(css))
	out = strings.ReplaceAll(out, "{{KATEX}}", runtime)
	out = strings.ReplaceAll(out, "{{CONTENT}}", content)
	if runtime != "" && !hasKaTeXPlaceholder {
		out = injectKaTeXRuntime(out, runtime)
	}
	result.HTML = out
	if collector != nil {
		result.Metadata.Dependencies = collector.dependencies
		result.Metadata.Links = collector.links
		result.Metadata.Assets = collector.assets
		result.Metadata.Diagnostics = collector.diagnostics
	}
	return result, nil
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
	protected, mathBlocks := protectFencedDisplayMath(s)
	md := newMarkdown(hardwrap)
	var out bytes.Buffer
	if err := md.Convert([]byte(protected), &out); err != nil {
		return "", fmt.Errorf("render markdown: %w", err)
	}
	return processKaTeX(restoreFencedDisplayMath(out.String(), mathBlocks)), nil
}

func newMarkdown(hardwrap bool) goldmark.Markdown {
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
	return goldmark.New(rendererOptions...)
}

type fencedDisplayMath struct {
	placeholder string
	expression  string
}

// protectFencedDisplayMath removes Karte's multiline $$$ blocks before
// Goldmark can interpret TeX control characters as Markdown. The placeholders
// are HTML comments so they survive the Markdown pass without introducing an
// invalid paragraph around the restored block element.
func protectFencedDisplayMath(source string) (string, []fencedDisplayMath) {
	lines := strings.SplitAfter(source, "\n")
	prefix := "KARTE_MATH_BLOCK"
	for strings.Contains(source, prefix) {
		prefix += "_"
	}

	var out strings.Builder
	blocks := make([]fencedDisplayMath, 0)
	codeFence := byte(0)
	codeFenceLength := 0
	rawHTMLCodeContainer := ""
	htmlComment := false
	for i := 0; i < len(lines); {
		line := lineWithoutEnding(lines[i])
		if htmlComment {
			out.WriteString(lines[i])
			if strings.Contains(line, "-->") {
				htmlComment = false
			}
			i++
			continue
		}
		if commentStart := strings.Index(line, "<!--"); commentStart >= 0 {
			out.WriteString(lines[i])
			if !strings.Contains(line[commentStart+4:], "-->") {
				htmlComment = true
			}
			i++
			continue
		}
		if codeFence != 0 {
			out.WriteString(lines[i])
			if isClosingMarkdownFence(line, codeFence, codeFenceLength) {
				codeFence = 0
				codeFenceLength = 0
			}
			i++
			continue
		}
		if rawHTMLCodeContainer != "" {
			out.WriteString(lines[i])
			if closesRawHTMLCodeContainer(line, rawHTMLCodeContainer) {
				rawHTMLCodeContainer = ""
			}
			i++
			continue
		}
		if marker, length, ok := openingMarkdownFence(line); ok {
			codeFence = marker
			codeFenceLength = length
			out.WriteString(lines[i])
			i++
			continue
		}
		if container := openingRawHTMLCodeContainer(line); container != "" {
			rawHTMLCodeContainer = container
			out.WriteString(lines[i])
			i++
			continue
		}
		if !isDisplayMathFenceAt(lines, i) {
			out.WriteString(lines[i])
			i++
			continue
		}

		closing := -1
		for j := i + 1; j < len(lines); j++ {
			if isDisplayMathFenceLine(lineWithoutEnding(lines[j])) {
				closing = j
				break
			}
		}
		if closing < 0 {
			out.WriteString(lines[i])
			i++
			continue
		}

		var expression strings.Builder
		for j := i + 1; j < closing; j++ {
			expression.WriteString(lines[j])
		}
		placeholder := fmt.Sprintf("<!--%s_%d-->", prefix, len(blocks))
		blocks = append(blocks, fencedDisplayMath{
			placeholder: placeholder,
			expression:  strings.TrimSpace(expression.String()),
		})
		out.WriteString(leadingSpaces(line))
		out.WriteString(placeholder)
		out.WriteString(lineEnding(lines[closing]))
		i = closing + 1
	}
	return out.String(), blocks
}

func openingRawHTMLCodeContainer(line string) string {
	match := rawHTMLCodeOpenRe.FindStringSubmatchIndex(line)
	if match == nil {
		return ""
	}
	tag := strings.ToLower(line[match[2]:match[3]])
	if closesRawHTMLCodeContainer(line[match[1]:], tag) {
		return ""
	}
	return tag
}

func closesRawHTMLCodeContainer(line, tag string) bool {
	lower := strings.ToLower(line)
	return strings.Contains(lower, "</"+tag+">") || strings.Contains(lower, "</"+tag+" >")
}

func leadingSpaces(line string) string {
	spaces := 0
	for spaces < len(line) && (line[spaces] == ' ' || line[spaces] == '\t') {
		spaces++
	}
	return line[:spaces]
}

func restoreFencedDisplayMath(rendered string, blocks []fencedDisplayMath) string {
	for _, block := range blocks {
		escaped := html.EscapeString(block.expression)
		replacement := `<div class="katex-display" data-katex="` + escaped + `">` + escaped + `</div>`
		rendered = strings.ReplaceAll(rendered, block.placeholder, replacement)
	}
	return rendered
}

func lineWithoutEnding(line string) string {
	line = strings.TrimSuffix(line, "\n")
	return strings.TrimSuffix(line, "\r")
}

func lineEnding(line string) string {
	if strings.HasSuffix(line, "\r\n") {
		return "\r\n"
	}
	if strings.HasSuffix(line, "\n") {
		return "\n"
	}
	return ""
}

func markdownFencePrefix(line string) string {
	spaces := 0
	for spaces < len(line) && line[spaces] == ' ' {
		spaces++
	}
	if spaces > 3 {
		return ""
	}
	return line[spaces:]
}

func openingMarkdownFence(line string) (byte, int, bool) {
	line = markdownFencePrefix(line)
	if line == "" || (line[0] != '`' && line[0] != '~') {
		return 0, 0, false
	}
	marker := line[0]
	length := 0
	for length < len(line) && line[length] == marker {
		length++
	}
	return marker, length, length >= 3
}

func isClosingMarkdownFence(line string, marker byte, minimumLength int) bool {
	line = markdownFencePrefix(line)
	if line == "" || line[0] != marker {
		return false
	}
	length := 0
	for length < len(line) && line[length] == marker {
		length++
	}
	return length >= minimumLength && strings.TrimSpace(line[length:]) == ""
}

func isDisplayMathFenceLine(line string) bool {
	return strings.TrimSpace(line) == "$$$"
}

func isDisplayMathFenceAt(lines []string, index int) bool {
	line := lineWithoutEnding(lines[index])
	if !isDisplayMathFenceLine(line) {
		return false
	}
	indent := markdownIndentColumns(line)
	if indent <= 3 {
		return true
	}
	for previous := index - 1; previous >= 0; previous-- {
		candidate := lineWithoutEnding(lines[previous])
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		if contentIndent, ok := markdownListContentIndent(candidate); ok && contentIndent <= indent {
			return true
		}
		if markdownIndentColumns(candidate) == 0 {
			return false
		}
	}
	return false
}

func markdownIndentColumns(line string) int {
	columns := 0
	for _, char := range line {
		switch char {
		case ' ':
			columns++
		case '\t':
			columns += 4 - columns%4
		default:
			return columns
		}
	}
	return columns
}

func markdownListContentIndent(line string) (int, bool) {
	indent := markdownIndentColumns(line)
	trimmed := strings.TrimLeft(line, " \t")
	markerLength := 0
	if len(trimmed) >= 2 && strings.ContainsRune("-+*", rune(trimmed[0])) && (trimmed[1] == ' ' || trimmed[1] == '\t') {
		markerLength = 1
	} else {
		for markerLength < len(trimmed) && trimmed[markerLength] >= '0' && trimmed[markerLength] <= '9' {
			markerLength++
		}
		if markerLength == 0 || markerLength >= len(trimmed) || (trimmed[markerLength] != '.' && trimmed[markerLength] != ')') {
			return 0, false
		}
		markerLength++
		if markerLength >= len(trimmed) || (trimmed[markerLength] != ' ' && trimmed[markerLength] != '\t') {
			return 0, false
		}
	}
	contentIndent := indent + markerLength
	for contentIndent-indent < len(trimmed) {
		char := trimmed[contentIndent-indent]
		if char == ' ' {
			contentIndent++
			continue
		}
		if char == '\t' {
			contentIndent += 4 - contentIndent%4
		}
		break
	}
	return contentIndent, true
}

func (r *Renderer) expandImports(root, baseDir, s string, hardwrap bool) (string, error) {
	return r.expandImportsWithMetadata(root, baseDir, s, hardwrap, nil)
}

func (r *Renderer) expandImportsWithMetadata(root, baseDir, s string, hardwrap bool, collector *metadataCollector) (string, error) {
	return r.expandImportsRecursive(root, baseDir, s, hardwrap, map[string]bool{}, 0, collector)
}

func (r *Renderer) expandImportsRecursive(root, baseDir, s string, hardwrap bool, stack map[string]bool, depth int, collector *metadataCollector) (string, error) {
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
		if collector != nil {
			collector.addDependency(dependencyKindForImport(typ), full)
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
			nested, err := r.expandImportsRecursive(root, filepath.Dir(full), string(b), hardwrap, stack, depth+1, collector)
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
	s = protect(katexProtectedCommentRe, s)
	s = protect(katexProtectedPreRe, s)
	s = protect(katexProtectedCodeRe, s)
	s = katexDisplayRe.ReplaceAllStringFunc(s, func(m string) string {
		expr := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(m, "$$$"), "$$$"))
		expr = html.UnescapeString(expr)
		return `<div class="katex-display" data-katex="` + html.EscapeString(expr) + `">` + html.EscapeString(expr) + `</div>`
	})
	s = katexInlineRe.ReplaceAllStringFunc(s, func(m string) string {
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
	layout, _, err := r.loadLayoutWithPath(root)
	return layout, err
}

func (r *Renderer) loadLayoutWithPath(root string) (string, string, error) {
	for _, name := range []string{"preview.html", "layout.html"} {
		p := filepath.Join(root, "themes", "default", name)
		b, err := r.fs.ReadFile(p)
		if err == nil {
			return string(b), p, nil
		}
		if !os.IsNotExist(err) {
			return "", "", err
		}
	}
	return fallbackLayout, "", nil
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
	type osPathFileSystem interface {
		UsesOSPaths() bool
	}
	fs, ok := r.fs.(osPathFileSystem)
	return ok && fs.UsesOSPaths()
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
