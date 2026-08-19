package renderer

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDetectMarpSectionBoundariesFixture(t *testing.T) {
	source := readPreviewFixture(t, "marp-complex.md")
	boundaries, err := DetectMarpSectionBoundaries(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(boundaries) != 5 {
		t.Fatalf("boundaries = %#v, want 5 slides", boundaries)
	}
	bodies := make([]string, len(boundaries))
	for i, boundary := range boundaries {
		bodies[i] = source[boundary.StartByte:boundary.EndByte]
	}
	if strings.Contains(bodies[0], "marp: true") {
		t.Fatalf("front matter leaked into first slide: %q", bodies[0])
	}
	if !strings.Contains(bodies[0], "```markdown\n---\n```") || !strings.Contains(bodies[0], "<!--\n---\n-->") {
		t.Fatalf("fence or comment delimiter split a slide: %q", bodies[0])
	}
	if boundaries[0].ExplicitID != "intro.slide" {
		t.Fatalf("explicit ID = %q", boundaries[0].ExplicitID)
	}
	if bodies[2] != "" {
		t.Fatalf("empty slide body = %q", bodies[2])
	}
	if bodies[3] != bodies[4] {
		t.Fatalf("identical fixtures differ: %q != %q", bodies[3], bodies[4])
	}
}

func TestBuildMarpPreviewPagesCoordinatesAndIDs(t *testing.T) {
	source := "日本😀\r\n---\r\n次\r\n"
	boundaries, err := DetectMarpSectionBoundaries(source)
	if err != nil {
		t.Fatal(err)
	}
	pages, err := BuildMarpPreviewPages(`slides\deck.md`, source, boundaries)
	if err != nil {
		t.Fatal(err)
	}
	wantRanges := []PreviewSourceRange{
		{Path: "slides/deck.md", StartByte: 0, EndByte: 12, StartUTF16: 0, EndUTF16: 6, StartLine: 0, EndLine: 1},
		{Path: "slides/deck.md", StartByte: 17, EndByte: 22, StartUTF16: 11, EndUTF16: 14, StartLine: 2, EndLine: 3},
	}
	for i, want := range wantRanges {
		if pages[i].Source == nil || !reflect.DeepEqual(*pages[i].Source, want) {
			t.Fatalf("page %d range = %#v, want %#v", i, pages[i].Source, want)
		}
		if pages[i].Index != i || !pages[i].Editable {
			t.Fatalf("page %d metadata = %#v", i, pages[i])
		}
	}
	if pages[0].ID == pages[1].ID || !strings.HasPrefix(pages[0].ID, "marp-") {
		t.Fatalf("unexpected IDs: %#v", pages)
	}
}

func TestBuildMarpPreviewPagesDeterministicDuplicateOrdinal(t *testing.T) {
	source := "# Same\n---\n# Same\n"
	boundaries, err := DetectMarpSectionBoundaries(source)
	if err != nil {
		t.Fatal(err)
	}
	first, err := BuildMarpPreviewPages("deck.md", source, boundaries)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildMarpPreviewPages("deck.md", source, boundaries)
	if err != nil {
		t.Fatal(err)
	}
	if first[0].ID != second[0].ID || first[1].ID != second[1].ID {
		t.Fatalf("IDs are not deterministic: %#v != %#v", first, second)
	}
	base0 := strings.TrimSuffix(first[0].ID, "-1")
	base1 := strings.TrimSuffix(first[1].ID, "-2")
	if base0 != base1 || first[0].ID == first[1].ID {
		t.Fatalf("duplicate ordinal IDs = %q, %q", first[0].ID, first[1].ID)
	}
}

func TestBuildMarpPreviewPagesExplicitIDAndUnmapped(t *testing.T) {
	boundaries := []PreviewSectionBoundary{
		{StartByte: 0, EndByte: 1, ExplicitID: "stable"},
		{StartByte: 1, EndByte: 2, ExplicitID: "stable"},
		{ExplicitID: "imported", Unmapped: true},
	}
	pages, err := BuildMarpPreviewPages("deck.md", "ab", boundaries)
	if err != nil {
		t.Fatal(err)
	}
	if pages[0].ID != "stable" || pages[1].ID != "stable--2" {
		t.Fatalf("explicit ID collision was not resolved: %#v", pages)
	}
	if pages[2].Editable || pages[2].Source != nil || pages[2].ID != "imported" {
		t.Fatalf("unmapped page = %#v", pages[2])
	}
	boundaries[0].ExplicitID = "unsafe id"
	if _, err := BuildMarpPreviewPages("deck.md", "ab", boundaries); err == nil {
		t.Fatal("unsafe explicit page ID was accepted")
	}
}

func TestBuildMarpPreviewPagesFindsExplicitIDInBridgeBoundary(t *testing.T) {
	source := "<!-- karte-page-id: bridge.id -->\n# Slide\n"
	pages, err := BuildMarpPreviewPages("deck.md", source, []PreviewSectionBoundary{{EndByte: len(source)}})
	if err != nil {
		t.Fatal(err)
	}
	if pages[0].ID != "bridge.id" {
		t.Fatalf("page ID = %q, want bridge.id", pages[0].ID)
	}
}

func TestAttachPreviewPageMetadata(t *testing.T) {
	source := "a"
	pages, err := BuildMarpPreviewPages("a&b.md", source, []PreviewSectionBoundary{{EndByte: 1}, {ExplicitID: "external", Unmapped: true}})
	if err != nil {
		t.Fatal(err)
	}
	rendered := `<div class="marpit"><!-- <section></section> --><section class="one"><section>nested</section></section><section><script>"<section>"</script></section></div>`
	got, err := AttachPreviewPageMetadata(rendered, pages)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<section data-karte-page-id="`,
		`data-karte-page-index="0"`,
		`data-karte-page-editable="true"`,
		`data-karte-source-path="a&amp;b.md"`,
		`data-karte-source-start-byte="0"`,
		`data-karte-page-id="external" data-karte-page-index="1" data-karte-page-editable="false"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("metadata %q missing from %s", want, got)
		}
	}
	if strings.Count(got, "data-karte-page-id") != 2 {
		t.Fatalf("nested/comment/script sections were annotated: %s", got)
	}
	if _, err := AttachPreviewPageMetadata("<section></section>", pages); err == nil {
		t.Fatal("section/page count mismatch was accepted")
	}
}

func TestBuildMarkdownPreviewPages(t *testing.T) {
	mode, pages, err := BuildMarkdownPreviewPages("notes.md", "# Notes 😀")
	if err != nil {
		t.Fatal(err)
	}
	if mode != PreviewPageContinuous || len(pages) != 1 || pages[0].Source.EndUTF16 != 10 {
		t.Fatalf("continuous metadata = %q, %#v", mode, pages)
	}
	for _, source := range []string{"before\n@pagebreak\nafter", `<div class="karte-print-page">x</div>`} {
		mode, pages, err = BuildMarkdownPreviewPages("notes.md", source)
		if err != nil || mode != PreviewPageRuntime || pages != nil {
			t.Fatalf("runtime metadata for %q = %q, %#v, %v", source, mode, pages, err)
		}
	}
	mode, pages, err = BuildMarkdownPreviewPages("notes.md", "```md\n@pagebreak\n```")
	if err != nil || mode != PreviewPageContinuous || len(pages) != 1 {
		t.Fatalf("fenced marker metadata = %q, %#v, %v", mode, pages, err)
	}
}

func TestPreviewRangeRejectsInvalidCoordinates(t *testing.T) {
	if _, err := BuildPreviewSourceRange("x.md", "😀", 1, 4); err == nil {
		t.Fatal("offset inside a UTF-8 rune was accepted")
	}
	if _, err := DetectMarpSectionBoundaries(string([]byte{0xff})); err == nil {
		t.Fatal("invalid UTF-8 source was accepted")
	}
}

func TestDetectMarpSectionBoundariesCRLFFixture(t *testing.T) {
	source := readPreviewFixture(t, "marp-crlf.md")
	if !strings.Contains(source, "\r\n") {
		t.Fatal("CRLF fixture lost CRLF line endings")
	}
	boundaries, err := DetectMarpSectionBoundaries(source)
	if err != nil || len(boundaries) != 2 {
		t.Fatalf("CRLF boundaries = %#v, %v", boundaries, err)
	}
	pages, err := BuildMarpPreviewPages("crlf.md", source, boundaries)
	if err != nil {
		t.Fatal(err)
	}
	if pages[0].Source.StartLine != 3 || pages[0].Source.EndLine != 5 || pages[1].Source.StartLine != 6 {
		t.Fatalf("CRLF line ranges = %#v", pages)
	}
}

func readPreviewFixture(t *testing.T, name string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", "preview", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
