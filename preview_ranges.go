package renderer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// PreviewSectionBoundary describes a rendered section in the original UTF-8
// source. Byte offsets are end-exclusive. Unmapped pages are read-only and do
// not expose a misleading source range.
type PreviewSectionBoundary struct {
	StartByte  int
	EndByte    int
	ExplicitID string
	Unmapped   bool
}

// DetectMarpSectionBoundaries finds slide bodies when Marpit token maps are not
// available. Front matter and separator lines are excluded, and empty slides
// are preserved. Fenced code and HTML comments are scanned as lexical states.
func DetectMarpSectionBoundaries(source string) ([]PreviewSectionBoundary, error) {
	if !utf8.ValidString(source) {
		return nil, fmt.Errorf("preview source is not valid UTF-8")
	}
	lines := previewLines(source)
	bodyStart, firstLine := previewBodyStart(source, lines)
	start := bodyStart
	var boundaries []PreviewSectionBoundary
	var fence previewFence
	inComment := false
	for _, line := range lines[firstLine:] {
		text := source[line.start:line.contentEnd]
		if fence.marker != 0 {
			if previewFenceClose(text, fence) {
				fence = previewFence{}
			}
			continue
		}
		if inComment {
			if strings.Contains(text, "-->") {
				inComment = false
			}
			continue
		}
		if next, ok := previewFenceOpen(text); ok {
			fence = next
			continue
		}
		if commentStart := strings.Index(text, "<!--"); commentStart >= 0 {
			inComment = !strings.Contains(text[commentStart+4:], "-->")
			continue
		}
		if previewSeparator(text) {
			boundaries = append(boundaries, PreviewSectionBoundary{StartByte: start, EndByte: line.start})
			start = line.end
		}
	}
	boundaries = append(boundaries, PreviewSectionBoundary{StartByte: start, EndByte: len(source)})
	for i := range boundaries {
		boundaries[i].ExplicitID = explicitKartePageID(source[boundaries[i].StartByte:boundaries[i].EndByte])
	}
	return boundaries, nil
}

// BuildMarpPreviewPages calculates byte, UTF-16, and line ranges and assigns a
// deterministic ID to every section boundary.
func BuildMarpPreviewPages(path, source string, boundaries []PreviewSectionBoundary) ([]PreviewPage, error) {
	return buildPreviewPages("marp", path, source, boundaries)
}

// BuildPreviewSourceRange constructs all coordinates used by the contract.
func BuildPreviewSourceRange(path, source string, startByte, endByte int) (PreviewSourceRange, error) {
	pages, err := buildPreviewPages("range", path, source, []PreviewSectionBoundary{{StartByte: startByte, EndByte: endByte}})
	if err != nil {
		return PreviewSourceRange{}, err
	}
	return *pages[0].Source, nil
}

// BuildMarkdownPreviewPages reports only pagination known before browser
// layout. Explicit pagination markers select runtime-pages without inventing
// physical ranges; otherwise the document is continuous and has no physical
// page list yet.
func BuildMarkdownPreviewPages(path, source string) (PreviewPageMode, []PreviewPage, error) {
	if hasRuntimePaginationMarker(source) {
		return PreviewPageRuntime, nil, nil
	}
	return PreviewPageContinuous, nil, nil
}

func buildPreviewPages(prefix, path, source string, boundaries []PreviewSectionBoundary) ([]PreviewPage, error) {
	if !utf8.ValidString(source) {
		return nil, fmt.Errorf("preview source is not valid UTF-8")
	}
	var offsets []int
	previousEnd := 0
	haveMapped := false
	for i, boundary := range boundaries {
		if boundary.ExplicitID != "" && !validExplicitPageID(boundary.ExplicitID) {
			return nil, fmt.Errorf("preview section %d has unsafe explicit page ID %q", i, boundary.ExplicitID)
		}
		if boundary.Unmapped {
			continue
		}
		if boundary.StartByte < 0 || boundary.StartByte > boundary.EndByte || boundary.EndByte > len(source) {
			return nil, fmt.Errorf("preview section %d has invalid source range [%d,%d)", i, boundary.StartByte, boundary.EndByte)
		}
		if haveMapped && boundary.StartByte < previousEnd {
			return nil, fmt.Errorf("preview section %d overlaps the previous mapped section", i)
		}
		if !previewRuneBoundary(source, boundary.StartByte) || !previewRuneBoundary(source, boundary.EndByte) {
			return nil, fmt.Errorf("preview section %d is not aligned to UTF-8 rune boundaries", i)
		}
		offsets = append(offsets, boundary.StartByte, boundary.EndByte)
		previousEnd, haveMapped = boundary.EndByte, true
	}
	coordinates := newPreviewCoordinates(source, offsets)
	path = strings.ReplaceAll(filepath.ToSlash(path), "\\", "/")
	ordinals := map[string]int{}
	used := map[string]bool{}
	pages := make([]PreviewPage, 0, len(boundaries))
	for index, boundary := range boundaries {
		id := strings.TrimSpace(boundary.ExplicitID)
		if id == "" && !boundary.Unmapped {
			id = explicitKartePageID(source[boundary.StartByte:boundary.EndByte])
		}
		if id == "" && boundary.Unmapped {
			id = prefix + "-unmapped-" + strconv.Itoa(index+1)
		} else if id == "" {
			digest := sha256.Sum256([]byte(source[boundary.StartByte:boundary.EndByte]))
			fingerprint := hex.EncodeToString(digest[:])[:24]
			ordinals[fingerprint]++
			id = prefix + "-" + fingerprint + "-" + strconv.Itoa(ordinals[fingerprint])
		}
		id = uniquePreviewPageID(id, used)
		page := PreviewPage{ID: id, Index: index, Editable: !boundary.Unmapped}
		if !boundary.Unmapped {
			rng := coordinates.sourceRange(path, boundary.StartByte, boundary.EndByte)
			page.Source = &rng
		}
		pages = append(pages, page)
	}
	return pages, nil
}

type previewCoordinates struct {
	utf16At    map[int]int
	lineStarts []int
}

func newPreviewCoordinates(source string, offsets []int) previewCoordinates {
	offsets = append([]int(nil), offsets...)
	sort.Ints(offsets)
	utf16At := make(map[int]int, len(offsets))
	byteOffset, utf16Offset := 0, 0
	for _, target := range offsets {
		for byteOffset < target {
			r, size := utf8.DecodeRuneInString(source[byteOffset:])
			byteOffset += size
			if r > 0xffff {
				utf16Offset += 2
			} else {
				utf16Offset++
			}
		}
		utf16At[target] = utf16Offset
	}
	lineStarts := []int{0}
	for i := 0; i < len(source); {
		switch source[i] {
		case '\r':
			i++
			if i < len(source) && source[i] == '\n' {
				i++
			}
			lineStarts = append(lineStarts, i)
		case '\n':
			i++
			lineStarts = append(lineStarts, i)
		default:
			_, size := utf8.DecodeRuneInString(source[i:])
			i += size
		}
	}
	return previewCoordinates{utf16At: utf16At, lineStarts: lineStarts}
}

func (c previewCoordinates) sourceRange(path string, start, end int) PreviewSourceRange {
	startLine := sort.Search(len(c.lineStarts), func(i int) bool { return c.lineStarts[i] > start }) - 1
	if startLine < 0 {
		startLine = 0
	}
	endLine := startLine
	if end > start {
		endLine = sort.SearchInts(c.lineStarts, end)
	}
	return PreviewSourceRange{Path: path, StartByte: start, EndByte: end, StartUTF16: c.utf16At[start], EndUTF16: c.utf16At[end], StartLine: startLine, EndLine: endLine}
}

func previewRuneBoundary(source string, offset int) bool {
	return offset >= 0 && offset <= len(source) && (offset == len(source) || source[offset]&0xc0 != 0x80)
}

type previewLine struct{ start, contentEnd, end int }

func previewLines(source string) []previewLine {
	var lines []previewLine
	for start := 0; start < len(source); {
		contentEnd := start
		for contentEnd < len(source) && source[contentEnd] != '\r' && source[contentEnd] != '\n' {
			contentEnd++
		}
		end := contentEnd
		if end < len(source) && source[end] == '\r' {
			end++
			if end < len(source) && source[end] == '\n' {
				end++
			}
		} else if end < len(source) {
			end++
		}
		lines = append(lines, previewLine{start, contentEnd, end})
		start = end
	}
	return lines
}

func previewBodyStart(source string, lines []previewLine) (int, int) {
	if len(lines) == 0 || strings.Trim(strings.TrimPrefix(source[lines[0].start:lines[0].contentEnd], "\ufeff"), " \t") != "---" {
		return 0, 0
	}
	for i := 1; i < len(lines); i++ {
		if strings.Trim(source[lines[i].start:lines[i].contentEnd], " \t") == "---" {
			return lines[i].end, i + 1
		}
	}
	return 0, 0
}

func previewSeparator(line string) bool {
	indent := 0
	for indent < len(line) && line[indent] == ' ' {
		indent++
	}
	return indent <= 3 && strings.TrimRight(line[indent:], " \t") == "---"
}

type previewFence struct {
	marker byte
	length int
}

func previewFenceOpen(line string) (previewFence, bool) {
	rest, ok := previewIndented(line)
	if !ok || len(rest) < 3 || rest[0] != '`' && rest[0] != '~' {
		return previewFence{}, false
	}
	length := 0
	for length < len(rest) && rest[length] == rest[0] {
		length++
	}
	if length < 3 || rest[0] == '`' && strings.ContainsRune(rest[length:], '`') {
		return previewFence{}, false
	}
	return previewFence{rest[0], length}, true
}

func previewFenceClose(line string, fence previewFence) bool {
	rest, ok := previewIndented(line)
	if !ok {
		return false
	}
	length := 0
	for length < len(rest) && rest[length] == fence.marker {
		length++
	}
	return length >= fence.length && strings.Trim(rest[length:], " \t") == ""
}

func previewIndented(line string) (string, bool) {
	i := 0
	for i < len(line) && line[i] == ' ' {
		i++
		if i == 4 {
			return "", false
		}
	}
	return line[i:], true
}

func explicitKartePageID(source string) string {
	var fence previewFence
	for _, line := range previewLines(source) {
		text := source[line.start:line.contentEnd]
		if fence.marker != 0 {
			if previewFenceClose(text, fence) {
				fence = previewFence{}
			}
			continue
		}
		if next, ok := previewFenceOpen(text); ok {
			fence = next
			continue
		}
		for rest := text; ; {
			start := strings.Index(rest, "<!--")
			if start < 0 {
				break
			}
			rest = rest[start+4:]
			end := strings.Index(rest, "-->")
			if end < 0 {
				break
			}
			comment := strings.TrimSpace(rest[:end])
			if strings.HasPrefix(comment, "karte-page-id:") {
				id := strings.TrimSpace(strings.TrimPrefix(comment, "karte-page-id:"))
				if validExplicitPageID(id) {
					return id
				}
			}
			rest = rest[end+3:]
		}
	}
	return ""
}

func validExplicitPageID(id string) bool {
	if len(id) == 0 || len(id) > 128 || !previewAlphaNumeric(id[0]) {
		return false
	}
	for i := 1; i < len(id); i++ {
		if !previewAlphaNumeric(id[i]) && !strings.ContainsRune("._:-", rune(id[i])) {
			return false
		}
	}
	return true
}

func previewAlphaNumeric(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

func uniquePreviewPageID(base string, used map[string]bool) string {
	if !used[base] {
		used[base] = true
		return base
	}
	for ordinal := 2; ; ordinal++ {
		candidate := base + "--" + strconv.Itoa(ordinal)
		if !used[candidate] {
			used[candidate] = true
			return candidate
		}
	}
}

func hasRuntimePaginationMarker(source string) bool {
	if !utf8.ValidString(source) {
		return false
	}
	var fence previewFence
	inComment := false
	for _, line := range previewLines(source) {
		text := source[line.start:line.contentEnd]
		if fence.marker != 0 {
			if previewFenceClose(text, fence) {
				fence = previewFence{}
			}
			continue
		}
		if inComment {
			if strings.Contains(text, "-->") {
				inComment = false
			}
			continue
		}
		if next, ok := previewFenceOpen(text); ok {
			fence = next
			continue
		}
		if start := strings.Index(text, "<!--"); start >= 0 {
			inComment = !strings.Contains(text[start+4:], "-->")
			continue
		}
		trimmed := strings.TrimSpace(text)
		if trimmed == "@pagebreak" || strings.HasPrefix(trimmed, "@pagebreak(") && strings.HasSuffix(trimmed, ")") || strings.Contains(text, "karte-print-page") {
			return true
		}
	}
	return false
}

// AttachPreviewPageMetadata adds page identity and source coordinates to each
// outermost section in rendered Marp HTML.
func AttachPreviewPageMetadata(renderedHTML string, pages []PreviewPage) (string, error) {
	openings, err := outerPreviewSections(renderedHTML)
	if err != nil {
		return "", err
	}
	if len(openings) != len(pages) {
		return "", fmt.Errorf("Marp HTML has %d outer sections for %d preview pages", len(openings), len(pages))
	}
	var out strings.Builder
	last := 0
	for i, opening := range openings {
		out.WriteString(renderedHTML[last:opening])
		out.WriteString(previewPageAttributes(pages[i]))
		last = opening
	}
	out.WriteString(renderedHTML[last:])
	return out.String(), nil
}

func previewPageAttributes(page PreviewPage) string {
	attribute := func(name, value string) string { return " " + name + "=\"" + html.EscapeString(value) + "\"" }
	var out strings.Builder
	out.WriteString(attribute("data-karte-page-id", page.ID))
	out.WriteString(attribute("data-karte-page-index", strconv.Itoa(page.Index)))
	out.WriteString(attribute("data-karte-page-editable", strconv.FormatBool(page.Editable)))
	if page.Source == nil {
		return out.String()
	}
	if page.Source.Path != "" {
		out.WriteString(attribute("data-karte-source-path", page.Source.Path))
	}
	out.WriteString(attribute("data-karte-source-start-byte", strconv.Itoa(page.Source.StartByte)))
	out.WriteString(attribute("data-karte-source-end-byte", strconv.Itoa(page.Source.EndByte)))
	out.WriteString(attribute("data-karte-source-start-utf16", strconv.Itoa(page.Source.StartUTF16)))
	out.WriteString(attribute("data-karte-source-end-utf16", strconv.Itoa(page.Source.EndUTF16)))
	out.WriteString(attribute("data-karte-source-start-line", strconv.Itoa(page.Source.StartLine)))
	out.WriteString(attribute("data-karte-source-end-line", strconv.Itoa(page.Source.EndLine)))
	return out.String()
}

type previewHTMLTag struct {
	name                 string
	nameEnd, end         int
	closing, selfClosing bool
	comment              bool
}

func outerPreviewSections(source string) ([]int, error) {
	var openings []int
	depth := 0
	raw := ""
	for offset := 0; offset < len(source); {
		if raw != "" {
			closing := previewFindFold(source, offset, "</"+raw)
			if closing < 0 {
				return nil, fmt.Errorf("Marp HTML has unclosed <%s> element", raw)
			}
			offset, raw = closing, ""
		}
		next := strings.IndexByte(source[offset:], '<')
		if next < 0 {
			break
		}
		offset += next
		tag, ok := parsePreviewHTMLTag(source, offset)
		if !ok {
			offset++
			continue
		}
		if tag.name == "section" {
			if tag.closing {
				if depth > 0 {
					depth--
				}
			} else {
				if depth == 0 {
					if strings.Contains(strings.ToLower(source[offset:tag.end]), "data-karte-page-id") {
						return nil, fmt.Errorf("Marp section already has Karte page metadata")
					}
					openings = append(openings, tag.nameEnd)
				}
				if !tag.selfClosing {
					depth++
				}
			}
		}
		if !tag.closing && !tag.selfClosing && (tag.name == "script" || tag.name == "style" || tag.name == "textarea" || tag.name == "title") {
			raw = tag.name
		}
		offset = tag.end
	}
	return openings, nil
}

func parsePreviewHTMLTag(source string, offset int) (previewHTMLTag, bool) {
	if strings.HasPrefix(source[offset:], "<!--") {
		end := strings.Index(source[offset+4:], "-->")
		if end < 0 {
			return previewHTMLTag{end: len(source), comment: true}, true
		}
		return previewHTMLTag{end: offset + 4 + end + 3, comment: true}, true
	}
	i := offset + 1
	closing := false
	if i < len(source) && source[i] == '/' {
		closing, i = true, i+1
	}
	if i >= len(source) || !previewHTMLNameStart(source[i]) {
		return previewHTMLTag{}, false
	}
	start := i
	for i < len(source) && (previewHTMLNameStart(source[i]) || source[i] >= '0' && source[i] <= '9' || strings.ContainsRune(":-_", rune(source[i]))) {
		i++
	}
	nameEnd := i
	end := previewTagEnd(source, i)
	if end == 0 {
		return previewHTMLTag{}, false
	}
	last := end - 2
	for last >= i && strings.ContainsRune(" \t\r\n", rune(source[last])) {
		last--
	}
	return previewHTMLTag{name: strings.ToLower(source[start:nameEnd]), nameEnd: nameEnd, end: end, closing: closing, selfClosing: last >= i && source[last] == '/'}, true
}

func previewTagEnd(source string, offset int) int {
	var quote byte
	for i := offset; i < len(source); i++ {
		if quote != 0 {
			if source[i] == quote {
				quote = 0
			}
			continue
		}
		if source[i] == '\'' || source[i] == '"' {
			quote = source[i]
		} else if source[i] == '>' {
			return i + 1
		}
	}
	return 0
}

func previewHTMLNameStart(b byte) bool { return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' }

func previewFindFold(source string, offset int, needle string) int {
	needle = strings.ToLower(needle)
	for i := offset; i+len(needle) <= len(source); i++ {
		if strings.ToLower(source[i:i+len(needle)]) == needle {
			return i
		}
	}
	return -1
}
