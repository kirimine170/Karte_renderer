package renderer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestP0AcceptanceRenderPipeline(t *testing.T) {
	root, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(root, "testdata", "p0", "acceptance.md")
	firstOutput := filepath.Join(t.TempDir(), "acceptance-first.html")
	secondOutput := filepath.Join(t.TempDir(), "acceptance-second.html")

	firstFM, err := ConvertFile(context.Background(), input, firstOutput, ConvertOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if firstFM.Printout.Size != "B5" || firstFM.Printout.ExpectedPages != 48 {
		t.Fatalf("unexpected P0 printout contract: %+v", firstFM.Printout)
	}
	if _, err := ConvertFile(context.Background(), input, secondOutput, ConvertOptions{Root: root}); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(firstOutput)
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(secondOutput)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("P0 fixture did not render deterministically")
	}

	html := string(first)
	for _, want := range []string{
		`<html data-printout="B5"`,
		`@page:left{margin-left:14mm;margin-right:20mm;@bottom-left{content:counter(page);}}`,
		`class="katex-display" data-katex="\begin{aligned}`,
		`class="karte-page-break karte-break-before"`,
		`data-chart-type="line"`,
		`href="#diagram">Figure 1</a>`,
		`href="#trend">Figure 2</a>`,
		`href="#summary">Table 1</a>`,
		`<span class="karte-caption-label">Figure 2.</span> Deterministic trend`,
		`id="karte-pagination-style"`,
		`id="karte-figure-style"`,
	} {
		assertContains(t, html, want)
	}
	for _, unwanted := range []string{"<p>$$$", "<p>@pagebreak", "<p>@chart", "<p>@figure", "<p>@table"} {
		if strings.Contains(html, unwanted) {
			t.Fatalf("unexpanded P0 directive %q remains in output:\n%s", unwanted, html)
		}
	}
}

func TestP0Acceptance48PageFixtureContract(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "preflight", "48-pages.md"))
	if err != nil {
		t.Fatal(err)
	}
	body, fm, err := parseFrontMatter(string(fixture))
	if err != nil {
		t.Fatal(err)
	}
	if fm.Printout.Size != "B5" || fm.Printout.ExpectedPages != 48 {
		t.Fatalf("48-page fixture lost its B5/exact-page contract: %+v", fm.Printout)
	}
	if pageBreaks := strings.Count(body, "@pagebreak"); pageBreaks != 47 {
		t.Fatalf("48-page fixture has %d explicit breaks, want 47", pageBreaks)
	}
}
