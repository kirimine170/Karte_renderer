package renderer

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// PrintoutOptions describes finite paged-media settings. It is used both by
// YAML front matter and by the resolved PDF configuration.
type PrintoutOptions struct {
	Size          string `yaml:"size"`
	Orientation   string `yaml:"orientation"`
	Margin        string `yaml:"margin"`
	InsideMargin  string `yaml:"insideMargin"`
	OutsideMargin string `yaml:"outsideMargin"`
	Header        string `yaml:"header"`
	Footer        string `yaml:"footer"`
	PageNumbers   *bool  `yaml:"pageNumbers"`
	ChapterStart  string `yaml:"chapterStart"`
}

// UnmarshalYAML accepts both `printout: B5` and the full mapping form.
func (p *PrintoutOptions) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		if node.Tag != "!!str" {
			return fmt.Errorf("printout shorthand must be a page-size string")
		}
		p.Size = node.Value
		return nil
	}
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("printout must be a page-size string or mapping")
	}
	type plain PrintoutOptions
	var decoded plain
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	*p = PrintoutOptions(decoded)
	return nil
}

func (p PrintoutOptions) empty() bool {
	return p.Size == "" && p.Orientation == "" && p.Margin == "" &&
		p.InsideMargin == "" && p.OutsideMargin == "" && p.Header == "" &&
		p.Footer == "" && p.PageNumbers == nil && p.ChapterStart == ""
}

var physicalLengthRe = regexp.MustCompile(`^(?:0|(?:\d+(?:\.\d+)?|\.\d+)(?:mm|cm|in|pt|pc))$`)

func resolvePrintoutOptions(frontMatter PrintoutOptions, explicit PDFOptions) (PrintoutOptions, error) {
	resolved := frontMatter
	if explicit.PageSize != "" {
		resolved.Size = explicit.PageSize
	}
	if explicit.Orientation != "" {
		resolved.Orientation = explicit.Orientation
	}
	if explicit.Margin != "" {
		resolved.Margin = explicit.Margin
	}
	if explicit.InsideMargin != "" {
		resolved.InsideMargin = explicit.InsideMargin
	}
	if explicit.OutsideMargin != "" {
		resolved.OutsideMargin = explicit.OutsideMargin
	}
	if explicit.Header != "" {
		resolved.Header = explicit.Header
	}
	if explicit.Footer != "" {
		resolved.Footer = explicit.Footer
	}
	if explicit.PageNumbers != nil {
		resolved.PageNumbers = explicit.PageNumbers
	}
	if explicit.ChapterStart != "" {
		resolved.ChapterStart = explicit.ChapterStart
	}

	if resolved.Size != "" {
		canonical, ok := map[string]string{
			"a3": "A3", "a4": "A4", "a5": "A5", "b4": "B4", "b5": "B5",
			"letter": "Letter", "legal": "Legal",
		}[strings.ToLower(strings.TrimSpace(resolved.Size))]
		if !ok {
			return PrintoutOptions{}, fmt.Errorf("invalid printout page size %q (use A3, A4, A5, B4, B5, Letter, or Legal)", resolved.Size)
		}
		resolved.Size = canonical
	}
	if resolved.Orientation != "" {
		resolved.Orientation = strings.ToLower(strings.TrimSpace(resolved.Orientation))
		if resolved.Orientation != "portrait" && resolved.Orientation != "landscape" {
			return PrintoutOptions{}, fmt.Errorf("invalid printout orientation %q (use portrait or landscape)", resolved.Orientation)
		}
	}
	if err := validateMargin("margin", resolved.Margin, 1, 4); err != nil {
		return PrintoutOptions{}, err
	}
	if err := validateMargin("inside margin", resolved.InsideMargin, 1, 1); err != nil {
		return PrintoutOptions{}, err
	}
	if err := validateMargin("outside margin", resolved.OutsideMargin, 1, 1); err != nil {
		return PrintoutOptions{}, err
	}
	resolved.Margin = strings.Join(strings.Fields(resolved.Margin), " ")
	resolved.InsideMargin = strings.TrimSpace(resolved.InsideMargin)
	resolved.OutsideMargin = strings.TrimSpace(resolved.OutsideMargin)
	resolved.ChapterStart = strings.ToLower(strings.TrimSpace(resolved.ChapterStart))
	switch resolved.ChapterStart {
	case "", "any":
	case "right", "recto":
		resolved.ChapterStart = "recto"
	default:
		return PrintoutOptions{}, fmt.Errorf("invalid chapter start %q (use right or any)", resolved.ChapterStart)
	}
	return resolved, nil
}

func validateMargin(name, value string, minimum, maximum int) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Fields(value)
	if len(parts) < minimum || len(parts) > maximum {
		return fmt.Errorf("invalid printout %s %q (expected %d to %d physical lengths)", name, value, minimum, maximum)
	}
	for _, part := range parts {
		if !physicalLengthRe.MatchString(part) {
			return fmt.Errorf("invalid printout %s length %q (use units mm, cm, in, pt, or pc)", name, part)
		}
	}
	return nil
}

func applyPrintoutOptions(document string, options PrintoutOptions) string {
	if options.empty() {
		return document
	}
	mode := options.Size
	if mode == "" {
		mode = "finite"
	}
	document = setHTMLAttribute(document, "data-printout", mode)
	style := buildPrintoutStyle(options)
	if style == "" {
		return document
	}
	return injectBeforeClosingTag(document, "head", `<style id="karte-printout-options">`+style+`</style>`)
}

func buildPrintoutStyle(options PrintoutOptions) string {
	var page, left, right, print strings.Builder
	if options.Size != "" || options.Orientation != "" {
		size := options.Size
		if size == "" {
			size = "A4"
		}
		if options.Orientation != "" {
			size += " " + options.Orientation
		}
		fmt.Fprintf(&page, "size:%s;", size)
	}
	if options.Margin != "" {
		fmt.Fprintf(&page, "margin:%s;", options.Margin)
	}
	if options.Header != "" {
		fmt.Fprintf(&page, "@top-center{content:%s;}", strconv.Quote(options.Header))
	}
	if options.Footer != "" {
		fmt.Fprintf(&page, "@bottom-center{content:%s;}", strconv.Quote(options.Footer))
	}
	if options.OutsideMargin != "" {
		fmt.Fprintf(&left, "margin-left:%s;", options.OutsideMargin)
		fmt.Fprintf(&right, "margin-right:%s;", options.OutsideMargin)
	}
	if options.InsideMargin != "" {
		fmt.Fprintf(&left, "margin-right:%s;", options.InsideMargin)
		fmt.Fprintf(&right, "margin-left:%s;", options.InsideMargin)
	}
	if options.PageNumbers != nil && *options.PageNumbers {
		left.WriteString("@bottom-left{content:counter(page);}")
		right.WriteString("@bottom-right{content:counter(page);}")
	}
	if options.ChapterStart == "recto" {
		print.WriteString("h1{break-before:recto;}")
	}

	var css strings.Builder
	if page.Len() > 0 {
		fmt.Fprintf(&css, "@page{%s}", page.String())
	}
	if left.Len() > 0 {
		fmt.Fprintf(&css, "@page:left{%s}", left.String())
	}
	if right.Len() > 0 {
		fmt.Fprintf(&css, "@page:right{%s}", right.String())
	}
	if print.Len() > 0 {
		fmt.Fprintf(&css, "@media print{%s}", print.String())
	}
	return css.String()
}

func setHTMLAttribute(document, name, value string) string {
	lower := strings.ToLower(document)
	at := strings.Index(lower, "<html")
	if at < 0 {
		return document
	}
	end := strings.Index(document[at:], ">")
	if end >= 0 {
		end += at
		openingTag := document[at:end]
		attribute := regexp.MustCompile(`(?i)(\s` + regexp.QuoteMeta(name) + `\s*=\s*)(?:"[^"]*"|'[^']*'|[^\s>]+)`)
		if attribute.MatchString(openingTag) {
			replaced := attribute.ReplaceAllString(openingTag, `${1}"`+html.EscapeString(value)+`"`)
			return document[:at] + replaced + document[end:]
		}
	}
	insertAt := at + len("<html")
	return document[:insertAt] + ` ` + name + `="` + html.EscapeString(value) + `"` + document[insertAt:]
}

func injectBeforeClosingTag(document, tag, content string) string {
	lower := strings.ToLower(document)
	closing := "</" + strings.ToLower(tag) + ">"
	if at := strings.Index(lower, closing); at >= 0 {
		return document[:at] + content + document[at:]
	}
	return content + document
}
