package renderer

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// FrontMatter contains YAML metadata found at the top of a Markdown document.
type FrontMatter struct {
	Title  string                 `yaml:"title"`
	Marp   bool                   `yaml:"marp"`
	Theme  string                 `yaml:"theme"`
	Layout string                 `yaml:"layout"`
	Data   map[string]interface{} `yaml:",inline"`
}

const fallbackLayout = `<!doctype html>
<html>
<head><meta charset="utf-8"><title>{{TITLE}}</title></head>
<body>{{CONTENT}}</body>
</html>`

var importRe = regexp.MustCompile(`(?m)^@import\(([^)]*)\)\s*$`)

// RenderMarkdown renders a Markdown file below root, returning HTML and front matter.
func RenderMarkdown(root string, path string) (html string, frontMatter FrontMatter, err error) {
	full, err := safeJoin(root, path)
	if err != nil {
		return "", FrontMatter{}, err
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return "", FrontMatter{}, err
	}
	return render(root, filepath.Dir(full), string(b))
}

// RenderString renders a Markdown string, returning HTML and front matter.
func RenderString(root string, markdown string) (html string, frontMatter FrontMatter, err error) {
	return render(root, root, markdown)
}

func render(root, baseDir, markdown string) (string, FrontMatter, error) {
	body, fm, err := parseFrontMatter(markdown)
	if err != nil {
		return "", fm, err
	}
	body, err = expandImports(root, baseDir, body)
	if err != nil {
		return "", fm, err
	}
	content, err := markdownHTML(body, fm.Marp)
	if err != nil {
		return "", fm, err
	}
	layout, err := loadLayout(root)
	if err != nil {
		return "", fm, err
	}
	out := strings.ReplaceAll(layout, "{{TITLE}}", html.EscapeString(fm.Title))
	out = strings.ReplaceAll(out, "{{CONTENT}}", content)
	return out, fm, nil
}

func parseFrontMatter(s string) (string, FrontMatter, error) {
	fm := FrontMatter{Data: map[string]interface{}{}}
	if strings.HasPrefix(s, "---\n") || strings.HasPrefix(s, "---\r\n") {
		nl := "\n"
		start := 4
		if strings.HasPrefix(s, "---\r\n") {
			nl = "\r\n"
			start = 5
		}
		endMarker := nl + "---" + nl
		end := strings.Index(s[start:], endMarker)
		if end < 0 {
			return "", fm, fmt.Errorf("unterminated YAML frontmatter")
		}
		yml := s[start : start+end]
		if err := parseYAML(yml, &fm); err != nil {
			return "", fm, err
		}
		return s[start+end+len(endMarker):], fm, nil
	}
	return s, fm, nil
}

func parseYAML(yml string, fm *FrontMatter) error {
	if fm.Data == nil {
		fm.Data = map[string]interface{}{}
	}
	scanner := bufio.NewScanner(strings.NewReader(yml))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			return fmt.Errorf("invalid frontmatter line %q", line)
		}
		key := strings.TrimSpace(k)
		val := strings.Trim(strings.TrimSpace(v), "\"'")
		var any interface{} = val
		if val == "true" {
			any = true
		} else if val == "false" {
			any = false
		}
		fm.Data[key] = any
		switch strings.ToLower(key) {
		case "title":
			fm.Title = val
		case "marp":
			fm.Marp = val == "true"
		case "theme":
			fm.Theme = val
		case "layout":
			fm.Layout = val
		}
	}
	return scanner.Err()
}

func simpleMarkdown(s string) string {
	lines := strings.Split(s, "\n")
	var sb strings.Builder
	var para []string
	flush := func() {
		if len(para) > 0 {
			sb.WriteString("<p>" + inlineMarkdown(strings.Join(para, "\n")) + "</p>\n")
			para = nil
		}
	}
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" {
			flush()
			continue
		}
		if strings.HasPrefix(trim, "<") {
			flush()
			sb.WriteString(trim + "\n")
			continue
		}
		if n := headingLevel(trim); n > 0 {
			flush()
			text := strings.TrimSpace(trim[n:])
			fmt.Fprintf(&sb, "<h%d>%s</h%d>\n", n, inlineMarkdown(text), n)
			continue
		}
		para = append(para, trim)
	}
	flush()
	return sb.String()
}

func headingLevel(s string) int {
	for i := 1; i <= 6 && i < len(s); i++ {
		if strings.HasPrefix(s, strings.Repeat("#", i)+" ") {
			return i
		}
	}
	return 0
}

func inlineMarkdown(s string) string {
	placeholders := map[string]string{}
	keep := regexp.MustCompile(`<span class="katex"[^>]*>.*?</span>`)
	s = keep.ReplaceAllStringFunc(s, func(m string) string {
		k := fmt.Sprintf("\x00%d\x00", len(placeholders))
		placeholders[k] = m
		return k
	})
	s = html.EscapeString(s)
	s = regexp.MustCompile(`\*\*([^*]+)\*\*`).ReplaceAllString(s, "<strong>$1</strong>")
	for k, v := range placeholders {
		s = strings.ReplaceAll(s, k, v)
	}
	return s
}

func expandImports(root, baseDir, s string) (string, error) {
	var firstErr error
	out := importRe.ReplaceAllStringFunc(s, func(m string) string {
		attrs := parseAttrs(importRe.FindStringSubmatch(m)[1])
		typ := attrs["type"]
		path := attrs["path"]
		if path == "" {
			path = attrs["src"]
		}
		if path == "" {
			firstErr = fmt.Errorf("@import missing path/src")
			return ""
		}
		full := path
		if !filepath.IsAbs(path) {
			full = filepath.Join(baseDir, path)
		}
		if !strings.HasPrefix(filepath.Clean(full), filepath.Clean(root)) {
			firstErr = fmt.Errorf("import path escapes root: %s", path)
			return ""
		}
		b, err := os.ReadFile(full)
		if err != nil {
			firstErr = err
			return ""
		}
		switch typ {
		case "csv":
			h, err := csvToHTML(b)
			if err != nil {
				firstErr = err
				return ""
			}
			return h
		case "md":
			nested, err := expandImports(root, filepath.Dir(full), string(b))
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

func csvToHTML(b []byte) (string, error) {
	recs, err := csv.NewReader(bytes.NewReader(b)).ReadAll()
	if err != nil {
		return "", err
	}
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
		for _, c := range r {
			fmt.Fprintf(&sb, "<%s>%s</%s>", cell, html.EscapeString(c), cell)
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

func markdownHTML(s string, marp bool) (string, error) {
	s = convertKatex(s)
	if marp {
		return renderSlides(s)
	}
	return simpleMarkdown(s), nil
}

func convertKatex(s string) string {
	blockRe := regexp.MustCompile(`(?s)\$\$\$(.+?)\$\$\$`)
	s = blockRe.ReplaceAllStringFunc(s, func(m string) string {
		expr := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(m, "$$$"), "$$$"))
		return `<div class="katex-display" data-katex="` + html.EscapeString(expr) + `">` + html.EscapeString(expr) + `</div>`
	})
	inlineRe := regexp.MustCompile(`\$([^$\n]+?)\$`)
	return inlineRe.ReplaceAllString(s, `<span class="katex" data-katex="$1">$1</span>`)
}

func renderSlides(s string) (string, error) {
	parts := regexp.MustCompile(`(?m)^---\s*$`).Split(s, -1)
	var sb strings.Builder
	for _, p := range parts {
		h := simpleMarkdown(strings.TrimSpace(p))
		sb.WriteString(`<section class="marp-slide">`)
		sb.WriteString(h)
		sb.WriteString(`</section>` + "\n")
	}
	return sb.String(), nil
}

func loadLayout(root string) (string, error) {
	for _, name := range []string{"preview.html", "layout.html"} {
		b, err := os.ReadFile(filepath.Join(root, "themes", "default", name))
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
	root = filepath.Clean(root)
	if full != root && !strings.HasPrefix(full, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes root: %s", p)
	}
	return full, nil
}
