package renderer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFiguresTablesChartsAndForwardReferencesAreNumbered(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "image.svg"), `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><circle cx="5" cy="5" r="4"/></svg>`)
	writeFile(t, filepath.Join(root, "data.csv"), "x,y\n1,2\n2,3\n")
	source := `See @ref(image), @ref(results), and @ref(trend).

@figure(id="image" src="image.svg" alt="Overview" caption="System overview" source="Internal" note="Simplified")

@table(id="results" caption="Estimated results" source="Performance log")
| Item | Value |
| --- | ---: |
| A | 10 |

@chart(type="line" path="data.csv" x="x" y="y" id="trend" caption="Profit trend" source="Performance log")`
	rendered, _, err := RenderString(root, source)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`href="#image">Figure 1</a>`,
		`href="#results">Table 1</a>`,
		`href="#trend">Figure 2</a>`,
		`<span class="karte-caption-label">Figure 1.</span> System overview`,
		`<span class="karte-caption-label">Table 1.</span> Estimated results`,
		`<span class="karte-caption-label">Figure 2.</span> Profit trend`,
		`<span class="karte-caption-source">Source: Performance log</span>`,
		`<span class="karte-caption-note">Note: Simplified</span>`,
		`id="karte-figure-style"`,
	} {
		assertContains(t, rendered, want)
	}
	if strings.Index(rendered, `id="results"`) > strings.Index(rendered, `<table>`) {
		t.Fatalf("annotated table was not wrapped by its numbered figure:\n%s", rendered)
	}
}

func TestFigureNumberingIsDeterministic(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "image.svg"), `<svg xmlns="http://www.w3.org/2000/svg"/>`)
	source := "@figure(id=\"one\" src=\"image.svg\" caption=\"One\")\n\n@figure(id=\"two\" src=\"image.svg\" caption=\"Two\")\n\n@ref(two)"
	first, _, err := RenderString(root, source)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := RenderString(root, source)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("figure numbering is not deterministic")
	}
	assertContains(t, first, `href="#two">Figure 2</a>`)
}

func TestFigureReferencesInsideCodeRemainLiteral(t *testing.T) {
	rendered, _, err := RenderString(t.TempDir(), "`@ref(example)`\n\n```md\n@ref(example)\n```")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(rendered, "@ref(example)") != 2 {
		t.Fatalf("code references changed:\n%s", rendered)
	}
}

func TestFigureDirectivesRejectInvalidReferences(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "image.svg"), `<svg xmlns="http://www.w3.org/2000/svg"/>`)
	for _, test := range []struct {
		name, source, want string
	}{
		{"missing id", `@figure(src="image.svg")`, "@figure missing id"},
		{"invalid id", `@figure(id="not valid" src="image.svg")`, "invalid figure reference id"},
		{"unknown ref", `See @ref(missing).`, `unknown figure reference "missing"`},
		{"table target", "@table(id=\"items\" caption=\"Items\")\n\nNot a table", "must be followed immediately"},
		{"traversal", `@figure(id="escape" src="../image.svg")`, "figure path escapes root"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := RenderString(root, test.source)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}
		})
	}
}

func TestDuplicateFigureReferenceIDFails(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "image.svg"), `<svg xmlns="http://www.w3.org/2000/svg"/>`)
	source := "@figure(id=\"same\" src=\"image.svg\")\n\n@figure(id=\"same\" src=\"image.svg\")"
	_, _, err := RenderString(root, source)
	if err == nil || !strings.Contains(err.Error(), `duplicate figure reference id "same"`) {
		t.Fatalf("expected duplicate id error, got %v", err)
	}
}

func TestFigureDirectiveRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.svg")
	writeFile(t, outside, `<svg xmlns="http://www.w3.org/2000/svg"/>`)
	if err := os.Symlink(outside, filepath.Join(root, "linked.svg")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, _, err := RenderString(root, `@figure(id="linked" src="linked.svg")`)
	if err == nil || !strings.Contains(err.Error(), "escapes root through symlink") {
		t.Fatalf("expected symlink escape error, got %v", err)
	}
}

func TestImportedFigureUsesResolvedPartialAssetURL(t *testing.T) {
	root := t.TempDir()
	partialDir := filepath.Join(root, "partials")
	asset := filepath.Join(partialDir, "assets", "figure.svg")
	if err := os.MkdirAll(filepath.Dir(asset), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, asset, `<svg xmlns="http://www.w3.org/2000/svg"/>`)
	writeFile(t, filepath.Join(partialDir, "chapter.md"), `@figure(id="nested" src="assets/figure.svg" caption="Nested")`)
	writeFile(t, filepath.Join(root, "book.md"), `@import(type="md" path="partials/chapter.md")`)

	rendered, _, err := RenderMarkdown(root, "book.md")
	if err != nil {
		t.Fatal(err)
	}
	resolvedAsset, err := filepath.EvalSymlinks(asset)
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, rendered, `src="`+fileURL(resolvedAsset)+`"`)
	if strings.Contains(rendered, `src="assets/figure.svg"`) {
		t.Fatalf("imported figure URL was resolved against the top-level document:\n%s", rendered)
	}
}

func TestFigureCaptionEscapesMetadata(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "image.svg"), `<svg xmlns="http://www.w3.org/2000/svg"/>`)
	rendered, _, err := RenderString(root, `@figure(id="safe" src="image.svg" caption="A < B" source="R&D")`)
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, rendered, "A &lt; B")
	assertContains(t, rendered, "R&amp;D")
}
