package renderer

import (
	"strings"
	"testing"
)

func TestPageBreakDirectivesRenderSemanticMarkers(t *testing.T) {
	rendered, _, err := RenderString(t.TempDir(), "# One\n\n@pagebreak\n\n# Two\n\n@pagebreak(after)\n\n# Three")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, rendered, `<div class="karte-page-break karte-break-before" role="separator" aria-label="Page break"></div>`)
	assertContains(t, rendered, `<div class="karte-page-break-after karte-break-after" role="separator" aria-label="Page break"></div>`)
	if strings.Contains(rendered, "<p>@pagebreak") {
		t.Fatalf("page-break directive was rendered as text:\n%s", rendered)
	}
}

func TestPageBreakDirectiveInsideCodeFenceRemainsLiteral(t *testing.T) {
	rendered, _, err := RenderString(t.TempDir(), "```md\n@pagebreak\n@pagebreak(after)\n```")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, rendered, "<code class=\"language-md\">@pagebreak")
	if strings.Contains(rendered, `role="separator"`) {
		t.Fatalf("code-fenced directive must remain literal:\n%s", rendered)
	}
}

func TestPaginationStyleProvidesKeepAndBreakRules(t *testing.T) {
	rendered, _, err := RenderString(t.TempDir(), "# Heading\n\n| A |\n| - |\n| 1 |")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`id="karte-pagination-style"`,
		`break-before:page`,
		`break-after:page`,
		`h1,h2,h3,h4,h5,h6,.karte-keep-with-next{break-after:avoid-page`,
		`figure,figcaption,img,table,pre,blockquote,.katex-display,.karte-keep-together{break-inside:avoid-page`,
		`thead{display:table-header-group;}`,
	} {
		assertContains(t, rendered, want)
	}
}

func TestPageBreakDirectiveSupportsCRLFAndIndentedForm(t *testing.T) {
	got := expandPageBreakDirectives("Before\r\n   @pagebreak(before)\r\nAfter\r\n")
	assertContains(t, got, `class="karte-page-break karte-break-before"`)
	if !strings.Contains(got, "</div>\r\nAfter") {
		t.Fatalf("CRLF was not preserved: %q", got)
	}
}
