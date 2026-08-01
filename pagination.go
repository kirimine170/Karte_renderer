package renderer

import (
	"regexp"
	"strings"
)

var pageBreakDirectiveRe = regexp.MustCompile(`^[ ]{0,3}@pagebreak(?:\(\s*(before|after)\s*\))?[ \t]*$`)
var paginationRawHTMLOpenRe = regexp.MustCompile(`(?i)<(pre|code|textarea|script|style)(?:\s[^>]*)?>`)

const paginationStyle = `@media print{
.karte-page-break,.karte-break-before{display:block;height:0;margin:0;border:0;break-before:page;page-break-before:always;}
.karte-page-break-after,.karte-break-after{display:block;height:0;margin:0;border:0;break-after:page;page-break-after:always;}
h1,h2,h3,h4,h5,h6,.karte-keep-with-next{break-after:avoid-page;page-break-after:avoid;}
figure,figcaption,img,table,pre,blockquote,.katex-display,.karte-keep-together{break-inside:avoid-page;page-break-inside:avoid;}
figcaption,caption{break-before:avoid-page;page-break-before:avoid;}
thead{display:table-header-group;}
}`

func expandPageBreakDirectives(source string) string {
	lines := strings.SplitAfter(source, "\n")
	var out strings.Builder
	codeFence := byte(0)
	codeFenceLength := 0
	rawHTMLContainer := ""
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
		if rawHTMLContainer != "" {
			out.WriteString(original)
			if closesPaginationRawHTML(line, rawHTMLContainer) {
				rawHTMLContainer = ""
			}
			continue
		}
		if marker, length, ok := paginationFenceOpen(line); ok {
			codeFence = marker
			codeFenceLength = length
			out.WriteString(original)
			continue
		}
		if container := opensPaginationRawHTML(line); container != "" {
			rawHTMLContainer = container
			out.WriteString(original)
			continue
		}
		match := pageBreakDirectiveRe.FindStringSubmatch(line)
		if match == nil {
			out.WriteString(original)
			continue
		}
		if match[1] == "after" {
			out.WriteString(`<div class="karte-page-break-after karte-break-after" role="separator" aria-label="Page break"></div>`)
		} else {
			out.WriteString(`<div class="karte-page-break karte-break-before" role="separator" aria-label="Page break"></div>`)
		}
		out.WriteString(ending)
	}
	return out.String()
}

func opensPaginationRawHTML(line string) string {
	match := paginationRawHTMLOpenRe.FindStringSubmatchIndex(line)
	if match == nil {
		return ""
	}
	tag := strings.ToLower(line[match[2]:match[3]])
	if closesPaginationRawHTML(line[match[1]:], tag) {
		return ""
	}
	return tag
}

func closesPaginationRawHTML(line, tag string) bool {
	return strings.Contains(strings.ToLower(line), "</"+tag+">")
}

func injectPaginationStyle(document string) string {
	if strings.Contains(document, `id="karte-pagination-style"`) {
		return document
	}
	style := `<style id="karte-pagination-style">` + paginationStyle + `</style>`
	if strings.Contains(strings.ToLower(document), "</head>") {
		return injectBeforeClosingTag(document, "head", style)
	}
	return document + style
}

func splitPaginationLine(line string) (string, string) {
	if strings.HasSuffix(line, "\r\n") {
		return strings.TrimSuffix(line, "\r\n"), "\r\n"
	}
	if strings.HasSuffix(line, "\n") {
		return strings.TrimSuffix(line, "\n"), "\n"
	}
	return line, ""
}

func paginationFencePrefix(line string) string {
	spaces := 0
	for spaces < len(line) && line[spaces] == ' ' {
		spaces++
	}
	if spaces > 3 {
		return ""
	}
	return line[spaces:]
}

func paginationFenceOpen(line string) (byte, int, bool) {
	line = paginationFencePrefix(line)
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

func isPaginationFenceClose(line string, marker byte, minimumLength int) bool {
	line = paginationFencePrefix(line)
	if line == "" || line[0] != marker {
		return false
	}
	length := 0
	for length < len(line) && line[length] == marker {
		length++
	}
	return length >= minimumLength && strings.TrimSpace(line[length:]) == ""
}
