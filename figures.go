package renderer

import (
	"fmt"
	"html"
	"path/filepath"
	"regexp"
	"strings"
)

var figureIDRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)
var tableDirectiveHTMLRe = regexp.MustCompile(`<div class="karte-table-directive" data-karte-id="([^"]*)" data-karte-caption="([^"]*)" data-karte-source="([^"]*)" data-karte-note="([^"]*)"></div>[ \t\r\n]*`)
var numberedFigureStartRe = regexp.MustCompile(`<figure\b[^>]*\bdata-karte-kind="(figure|table)"[^>]*>`)
var elementIDRe = regexp.MustCompile(`\bid="([A-Za-z][A-Za-z0-9_-]*)"`)
var figureRefRe = regexp.MustCompile(`@ref\(([A-Za-z][A-Za-z0-9_-]*)\)`)

type figureTarget struct {
	kind   string
	number int
}

const figureStyle = `<style id="karte-figure-style">
.karte-figure,.karte-chart-figure,.karte-table-figure{margin:1.2em 0;max-width:100%;}
.karte-figure>img,.karte-chart-figure svg{display:block;max-width:100%;height:auto;margin:0 auto;}
.karte-figure figcaption,.karte-chart-figure figcaption,.karte-table-figure figcaption{margin:.45em 0;font-size:.92em;line-height:1.45;}
.karte-caption-label{font-weight:700;}
.karte-caption-source,.karte-caption-note{display:block;font-size:.9em;color:#555;}
.karte-cross-reference{white-space:nowrap;}
@media print{.karte-figure,.karte-chart-figure,.karte-table-figure{break-inside:avoid-page;page-break-inside:avoid;}.karte-figure figcaption,.karte-chart-figure figcaption,.karte-table-figure figcaption{break-before:avoid-page;page-break-before:avoid;}}
</style>`

func (r *Renderer) expandFigureDirectives(root, baseDir, source string) (string, error) {
	lines := strings.SplitAfter(source, "\n")
	var out strings.Builder
	codeFence := byte(0)
	codeFenceLength := 0
	for _, original := range lines {
		line, ending := splitPaginationLine(original)
		if codeFence != 0 {
			out.WriteString(original)
			if isPaginationFenceClose(line, codeFence, codeFenceLength) {
				codeFence = 0
				codeFenceLength = 0
			}
			continue
		}
		if marker, length, ok := paginationFenceOpen(line); ok {
			codeFence = marker
			codeFenceLength = length
			out.WriteString(original)
			continue
		}
		trimmed := strings.TrimSpace(line)
		if len(line)-len(strings.TrimLeft(line, " ")) > 3 {
			out.WriteString(original)
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "@figure(") && strings.HasSuffix(trimmed, ")"):
			attrs := parseAttrs(strings.TrimSuffix(strings.TrimPrefix(trimmed, "@figure("), ")"))
			markup, err := r.renderImageFigure(root, baseDir, attrs)
			if err != nil {
				return "", err
			}
			out.WriteString(markup)
			out.WriteString(ending)
		case strings.HasPrefix(trimmed, "@table(") && strings.HasSuffix(trimmed, ")"):
			attrs := parseAttrs(strings.TrimSuffix(strings.TrimPrefix(trimmed, "@table("), ")"))
			id, err := requiredFigureID(attrs["id"], "@table")
			if err != nil {
				return "", err
			}
			blankLine := ending + ending
			if blankLine == "" {
				blankLine = "\n\n"
			}
			fmt.Fprintf(&out, `<div class="karte-table-directive" data-karte-id="%s" data-karte-caption="%s" data-karte-source="%s" data-karte-note="%s"></div>%s`, html.EscapeString(id), html.EscapeString(attrs["caption"]), html.EscapeString(attrs["source"]), html.EscapeString(attrs["note"]), blankLine)
		default:
			out.WriteString(original)
		}
	}
	return out.String(), nil
}

func (r *Renderer) renderImageFigure(root, baseDir string, attrs map[string]string) (string, error) {
	id, err := requiredFigureID(attrs["id"], "@figure")
	if err != nil {
		return "", err
	}
	sourcePath := strings.TrimSpace(attrs["src"])
	if sourcePath == "" {
		return "", fmt.Errorf("@figure missing src")
	}
	full := sourcePath
	if !filepath.IsAbs(full) {
		full = filepath.Join(baseDir, full)
	}
	full = filepath.Clean(full)
	if !isWithin(root, full) {
		return "", fmt.Errorf("figure path escapes root: %s", sourcePath)
	}
	if r.usesOSFileSystem() {
		full, err = resolveWithinRoot(root, full)
		if err != nil {
			return "", fmt.Errorf("resolve figure path %s: %w", sourcePath, err)
		}
	}
	if _, err := r.fs.Stat(full); err != nil {
		return "", fmt.Errorf("read figure asset %s: %w", sourcePath, err)
	}
	imageSource := fileURL(full)
	alt := attrs["alt"]
	if alt == "" {
		alt = attrs["caption"]
	}
	content := `<img src="` + html.EscapeString(imageSource) + `" alt="` + html.EscapeString(alt) + `">`
	return wrapNumberedFigure("figure", id, "karte-figure", content, attrs["caption"], attrs["source"], attrs["note"]), nil
}

func requiredFigureID(id, directive string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("%s missing id", directive)
	}
	if !figureIDRe.MatchString(id) {
		return "", fmt.Errorf("invalid figure reference id %q", id)
	}
	return id, nil
}

func optionalFigureID(id, directive string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", nil
	}
	if !figureIDRe.MatchString(id) {
		return "", fmt.Errorf("invalid %s id %q", directive, id)
	}
	return id, nil
}

func wrapNumberedFigure(kind, id, class, content, caption, source, note string) string {
	idAttribute := ""
	if id != "" {
		idAttribute = ` id="` + html.EscapeString(id) + `"`
	}
	return `<figure class="` + html.EscapeString(class) + `" data-karte-kind="` + html.EscapeString(kind) + `"` + idAttribute + `>` +
		content + figureCaption(caption, source, note) + `</figure>`
}

func figureCaption(caption, source, note string) string {
	var out strings.Builder
	out.WriteString(`<figcaption><span class="karte-caption-label" data-karte-caption-label></span>`)
	if caption != "" {
		out.WriteString(" ")
		out.WriteString(html.EscapeString(caption))
	}
	if source != "" {
		out.WriteString(`<span class="karte-caption-source">Source: `)
		out.WriteString(html.EscapeString(source))
		out.WriteString(`</span>`)
	}
	if note != "" {
		out.WriteString(`<span class="karte-caption-note">Note: `)
		out.WriteString(html.EscapeString(note))
		out.WriteString(`</span>`)
	}
	out.WriteString(`</figcaption>`)
	return out.String()
}

func finalizeFigures(content string) (string, error) {
	wrapped, err := wrapAnnotatedTables(content)
	if err != nil {
		return "", err
	}
	return numberFiguresAndResolveReferences(wrapped)
}

func wrapAnnotatedTables(content string) (string, error) {
	for {
		match := tableDirectiveHTMLRe.FindStringSubmatchIndex(content)
		if match == nil {
			return content, nil
		}
		tableStart := match[1]
		if !strings.HasPrefix(strings.ToLower(content[tableStart:]), "<table") {
			return "", fmt.Errorf("@table must be followed immediately by a Markdown table")
		}
		closingOffset := strings.Index(strings.ToLower(content[tableStart:]), "</table>")
		if closingOffset < 0 {
			return "", fmt.Errorf("@table target has no closing table element")
		}
		tableEnd := tableStart + closingOffset + len("</table>")
		id := html.UnescapeString(content[match[2]:match[3]])
		caption := html.UnescapeString(content[match[4]:match[5]])
		source := html.UnescapeString(content[match[6]:match[7]])
		note := html.UnescapeString(content[match[8]:match[9]])
		wrapped := wrapNumberedFigure("table", id, "karte-table-figure", content[tableStart:tableEnd], caption, source, note)
		content = content[:match[0]] + wrapped + content[tableEnd:]
	}
}

func numberFiguresAndResolveReferences(content string) (string, error) {
	targets := map[string]figureTarget{}
	counters := map[string]int{"figure": 0, "table": 0}
	var numbered strings.Builder
	cursor := 0
	for {
		location := numberedFigureStartRe.FindStringSubmatchIndex(content[cursor:])
		if location == nil {
			numbered.WriteString(content[cursor:])
			break
		}
		start := cursor + location[0]
		startEnd := cursor + location[1]
		kind := content[cursor+location[2] : cursor+location[3]]
		closeOffset := strings.Index(strings.ToLower(content[startEnd:]), "</figure>")
		if closeOffset < 0 {
			return "", fmt.Errorf("numbered %s has no closing figure element", kind)
		}
		end := startEnd + closeOffset + len("</figure>")
		block := content[start:end]
		counters[kind]++
		number := counters[kind]
		label := figureKindLabel(kind) + " " + fmt.Sprint(number) + "."
		block = strings.Replace(block, `<span class="karte-caption-label" data-karte-caption-label></span>`, `<span class="karte-caption-label">`+label+`</span>`, 1)
		if idMatch := elementIDRe.FindStringSubmatch(block); idMatch != nil {
			id := idMatch[1]
			if _, exists := targets[id]; exists {
				return "", fmt.Errorf("duplicate figure reference id %q", id)
			}
			targets[id] = figureTarget{kind: kind, number: number}
		}
		numbered.WriteString(content[cursor:start])
		numbered.WriteString(block)
		cursor = end
	}

	return replaceFigureReferences(numbered.String(), targets)
}

func replaceFigureReferences(content string, targets map[string]figureTarget) (string, error) {
	type protectedRegion struct{ placeholder, content string }
	protected := make([]protectedRegion, 0)
	protect := func(pattern *regexp.Regexp, input string) string {
		return pattern.ReplaceAllStringFunc(input, func(match string) string {
			placeholder := fmt.Sprintf("\x00KARTE_FIGURE_CODE_%d\x00", len(protected))
			protected = append(protected, protectedRegion{placeholder: placeholder, content: match})
			return placeholder
		})
	}
	content = protect(regexp.MustCompile(`(?s)<pre\b[^>]*>.*?</pre>`), content)
	content = protect(regexp.MustCompile(`(?s)<code\b[^>]*>.*?</code>`), content)
	var firstErr error
	content = figureRefRe.ReplaceAllStringFunc(content, func(reference string) string {
		if firstErr != nil {
			return reference
		}
		id := figureRefRe.FindStringSubmatch(reference)[1]
		target, ok := targets[id]
		if !ok {
			firstErr = fmt.Errorf("unknown figure reference %q", id)
			return reference
		}
		label := figureKindLabel(target.kind) + " " + fmt.Sprint(target.number)
		return `<a class="karte-cross-reference" href="#` + html.EscapeString(id) + `">` + label + `</a>`
	})
	for _, region := range protected {
		content = strings.ReplaceAll(content, region.placeholder, region.content)
	}
	return content, firstErr
}

func figureKindLabel(kind string) string {
	if kind == "table" {
		return "Table"
	}
	return "Figure"
}

func injectFigureStyle(document string) string {
	if !strings.Contains(document, `data-karte-kind=`) || strings.Contains(document, `id="karte-figure-style"`) {
		return document
	}
	if strings.Contains(strings.ToLower(document), "</head>") {
		return injectBeforeClosingTag(document, "head", figureStyle)
	}
	return document + figureStyle
}
