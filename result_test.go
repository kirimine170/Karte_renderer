package renderer

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRenderResultMetadataContract(t *testing.T) {
	root := filepath.Join("testdata", "render-result-metadata")
	result, err := RenderMarkdownResult(root, "document.md")
	if err != nil {
		t.Fatal(err)
	}

	if result.Metadata.SchemaVersion != RenderMetadataSchemaVersion {
		t.Fatalf("schema version = %d", result.Metadata.SchemaVersion)
	}
	if result.Metadata.FrontMatter.Title != "Render metadata fixture" {
		t.Fatalf("unexpected front matter: %+v", result.Metadata.FrontMatter)
	}
	if got := result.Metadata.FrontMatter.Data["project"]; got != "karte-format" {
		t.Fatalf("custom front matter was not preserved: %#v", result.Metadata.FrontMatter.Data)
	}

	wantDependencies := []RenderDependency{
		{Kind: DependencySource, Path: "document.md"},
		{Kind: DependencyMarkdownImport, Path: "partials/summary.md"},
		{Kind: DependencyCSVImport, Path: "data/metrics.csv"},
		{Kind: DependencyTeXImport, Path: "math/model.tex"},
	}
	if !reflect.DeepEqual(result.Metadata.Dependencies, wantDependencies) {
		t.Fatalf("dependencies = %#v, want %#v", result.Metadata.Dependencies, wantDependencies)
	}

	wantLinks := []RenderLink{
		{Kind: LinkExternal, Target: "https://example.com/spec"},
		{Kind: LinkInternal, Target: "notes.md#details", Path: "notes.md"},
		{Kind: LinkFragment, Target: "#summary"},
		{Kind: LinkEmail, Target: "mailto:team@example.com"},
		{Kind: LinkExternal, Target: "https://example.com/autolink"},
		{Kind: LinkExternal, Target: "https://example.com/bare"},
		{Kind: LinkEmail, Target: "team@example.com"},
		{Kind: LinkExternal, Target: "www.example.com"},
		{Kind: LinkInternal, Target: "?v=1", Path: "document.md"},
		{Kind: LinkInternal, Target: "", Path: "document.md"},
	}
	if !reflect.DeepEqual(result.Metadata.Links, wantLinks) {
		t.Fatalf("links = %#v, want %#v", result.Metadata.Links, wantLinks)
	}

	wantAssets := []RenderAsset{
		{Reference: "assets/diagram.svg", Path: "assets/diagram.svg", Status: AssetAvailable},
		{Reference: "assets/missing.png", Path: "assets/missing.png", Status: AssetMissing},
		{Reference: "https://example.com/logo.png", Status: AssetExternal},
		{Reference: "data:image/gif;base64,R0lGODlhAQABAAAAACw=", Status: AssetEmbedded},
	}
	if !reflect.DeepEqual(result.Metadata.Assets, wantAssets) {
		t.Fatalf("assets = %#v, want %#v", result.Metadata.Assets, wantAssets)
	}

	wantDiagnostics := []RenderDiagnostic{{
		Severity: DiagnosticWarning,
		Code:     "missing_asset",
		Message:  "asset \"assets/missing.png\" does not exist",
		Path:     "assets/missing.png",
	}}
	if !reflect.DeepEqual(result.Metadata.Diagnostics, wantDiagnostics) {
		t.Fatalf("diagnostics = %#v, want %#v", result.Metadata.Diagnostics, wantDiagnostics)
	}

	for _, want := range []string{"<h1>Metadata contract</h1>", "<h2>Summary</h2>", "<table>", `data-katex="x^2 + y^2"`} {
		assertContains(t, result.HTML, want)
	}

	encoded, err := json.Marshal(result.Metadata)
	if err != nil {
		t.Fatal(err)
	}
	jsonText := string(encoded)
	for _, want := range []string{`"schemaVersion":1`, `"frontMatter":{"title":"Render metadata fixture"`, `"dependencies"`, `"diagnostics"`} {
		if !strings.Contains(jsonText, want) {
			t.Fatalf("metadata JSON does not contain %q: %s", want, jsonText)
		}
	}
}

func TestRenderResultRejectsAssetOutsideRootWithoutFailingRender(t *testing.T) {
	root := t.TempDir()
	result, err := RenderStringResult(root, `![outside](../outside.png)`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Metadata.Assets) != 1 || result.Metadata.Assets[0].Status != AssetOutsideRoot {
		t.Fatalf("unexpected assets: %#v", result.Metadata.Assets)
	}
	if len(result.Metadata.Diagnostics) != 1 || result.Metadata.Diagnostics[0].Code != "asset_outside_root" {
		t.Fatalf("unexpected diagnostics: %#v", result.Metadata.Diagnostics)
	}
}

func TestRenderResultClassifiesMissingAssetBelowEscapingSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	result, err := RenderStringResult(root, `![outside](linked/missing.png)`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Metadata.Assets) != 1 || result.Metadata.Assets[0].Status != AssetOutsideRoot {
		t.Fatalf("unexpected assets: %#v", result.Metadata.Assets)
	}
	if len(result.Metadata.Diagnostics) != 1 || result.Metadata.Diagnostics[0].Code != "asset_outside_root" {
		t.Fatalf("unexpected diagnostics: %#v", result.Metadata.Diagnostics)
	}
}

func TestRenderResultClassifiesDanglingEscapingSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "missing.png")
	if err := os.Symlink(target, filepath.Join(root, "image.png")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	result, err := RenderStringResult(root, `![outside](image.png)`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Metadata.Assets) != 1 || result.Metadata.Assets[0].Status != AssetOutsideRoot {
		t.Fatalf("unexpected assets: %#v", result.Metadata.Assets)
	}
	if len(result.Metadata.Diagnostics) != 1 || result.Metadata.Diagnostics[0].Code != "asset_outside_root" {
		t.Fatalf("unexpected diagnostics: %#v", result.Metadata.Diagnostics)
	}
}

func TestRenderResultDoesNotExposeLinkPathThroughEscapingSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	result, err := RenderStringResult(root, `[secret](linked/secret.md)`)
	if err != nil {
		t.Fatal(err)
	}
	want := []RenderLink{{Kind: LinkInternal, Target: "linked/secret.md"}}
	if !reflect.DeepEqual(result.Metadata.Links, want) {
		t.Fatalf("links = %#v, want %#v", result.Metadata.Links, want)
	}
}

func TestRenderResultChecksSymlinksThroughOSFileSystemWrapper(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "secret.png"), "secret")
	if err := os.Symlink(filepath.Join(outside, "secret.png"), filepath.Join(root, "image.png")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	renderer := NewRenderer(osFileSystemWrapper{OSFileSystem: OSFileSystem{}})
	result, err := renderer.RenderStringResultWithOptions(root, `![secret](image.png)`, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []RenderAsset{{Reference: "image.png", Path: "image.png", Status: AssetOutsideRoot}}
	if !reflect.DeepEqual(result.Metadata.Assets, want) {
		t.Fatalf("assets = %#v, want %#v", result.Metadata.Assets, want)
	}
	if len(result.Metadata.Diagnostics) != 1 || result.Metadata.Diagnostics[0].Code != "asset_outside_root" {
		t.Fatalf("unexpected diagnostics: %#v", result.Metadata.Diagnostics)
	}
}

type osFileSystemWrapper struct {
	OSFileSystem
}

func TestRenderResultPreservesReferenceSourceOrderAcrossFootnotes(t *testing.T) {
	result, err := RenderStringResult(t.TempDir(), "[^x]: [foot](foot.md)\n\n[after](after.md)\n\nFootnote[^x]")
	if err != nil {
		t.Fatal(err)
	}
	want := []RenderLink{
		{Kind: LinkInternal, Target: "foot.md", Path: "foot.md"},
		{Kind: LinkInternal, Target: "after.md", Path: "after.md"},
	}
	if !reflect.DeepEqual(result.Metadata.Links, want) {
		t.Fatalf("links = %#v, want %#v", result.Metadata.Links, want)
	}
}

func TestRenderResultExcludesReferencesInsideMath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "real.png"), "real")
	result, err := RenderStringResult(root, `$![inline](inline.png)$

$$$
![display](display.png)
[link](inside.md)
$$$

![real](real.png)`)
	if err != nil {
		t.Fatal(err)
	}
	wantAssets := []RenderAsset{{Reference: "real.png", Path: "real.png", Status: AssetAvailable}}
	if !reflect.DeepEqual(result.Metadata.Assets, wantAssets) {
		t.Fatalf("assets = %#v, want %#v", result.Metadata.Assets, wantAssets)
	}
	if len(result.Metadata.Links) != 0 || len(result.Metadata.Diagnostics) != 0 {
		t.Fatalf("unexpected metadata: links=%#v diagnostics=%#v", result.Metadata.Links, result.Metadata.Diagnostics)
	}
}

func TestRenderResultExcludesReferencesPartiallyConsumedByMath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "foo$bar.png"), "image")
	writeFile(t, filepath.Join(root, "foo$bar.md"), "link")

	tests := []struct {
		name     string
		markdown string
	}{
		{name: "image destination", markdown: `![x](foo$bar.png) tail $`},
		{name: "link destination", markdown: `[x](foo$bar.md) tail $`},
		{name: "autolink", markdown: `<https://example.com/foo$bar> tail $`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := RenderStringResult(root, test.markdown)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Metadata.Assets) != 0 || len(result.Metadata.Links) != 0 || len(result.Metadata.Diagnostics) != 0 {
				t.Fatalf("unexpected metadata: assets=%#v links=%#v diagnostics=%#v", result.Metadata.Assets, result.Metadata.Links, result.Metadata.Diagnostics)
			}
		})
	}
}

func TestRenderResultKeepsAutolinksWithEncodedDollars(t *testing.T) {
	for _, markdown := range []string{
		`<https://example.com/foo&#36;bar>`,
		`<https://example.com/foo&#x24;bar>`,
	} {
		result, err := RenderStringResult(t.TempDir(), markdown)
		if err != nil {
			t.Fatal(err)
		}
		want := []RenderLink{{Kind: LinkExternal, Target: markdown[1 : len(markdown)-1]}}
		if !reflect.DeepEqual(result.Metadata.Links, want) {
			t.Fatalf("links = %#v, want %#v", result.Metadata.Links, want)
		}
	}
}

func TestRenderResultKeepsReferencesWithMathOnlyInLabels(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "notes.md"), "notes")
	writeFile(t, filepath.Join(root, "plot.png"), "plot")
	result, err := RenderStringResult(root, `[price $x$](notes.md)

![plot $x$](plot.png)`)
	if err != nil {
		t.Fatal(err)
	}
	wantLinks := []RenderLink{{Kind: LinkInternal, Target: "notes.md", Path: "notes.md"}}
	if !reflect.DeepEqual(result.Metadata.Links, wantLinks) {
		t.Fatalf("links = %#v, want %#v", result.Metadata.Links, wantLinks)
	}
	wantAssets := []RenderAsset{{Reference: "plot.png", Path: "plot.png", Status: AssetAvailable}}
	if !reflect.DeepEqual(result.Metadata.Assets, wantAssets) {
		t.Fatalf("assets = %#v, want %#v", result.Metadata.Assets, wantAssets)
	}
	if len(result.Metadata.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", result.Metadata.Diagnostics)
	}
}

func TestRenderResultKeepsReferencesWithMathOnlyInTitles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "notes.md"), "notes")
	writeFile(t, filepath.Join(root, "plot.png"), "plot")
	result, err := RenderStringResult(root, `[docs](notes.md "$x$")

![plot](plot.png "$x$")`)
	if err != nil {
		t.Fatal(err)
	}
	wantLinks := []RenderLink{{Kind: LinkInternal, Target: "notes.md", Path: "notes.md"}}
	if !reflect.DeepEqual(result.Metadata.Links, wantLinks) {
		t.Fatalf("links = %#v, want %#v", result.Metadata.Links, wantLinks)
	}
	wantAssets := []RenderAsset{{Reference: "plot.png", Path: "plot.png", Status: AssetAvailable}}
	if !reflect.DeepEqual(result.Metadata.Assets, wantAssets) {
		t.Fatalf("assets = %#v, want %#v", result.Metadata.Assets, wantAssets)
	}
}

func TestRenderResultKeepsReferenceIDsWithMath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "notes.md"), "notes")
	writeFile(t, filepath.Join(root, "plot.png"), "plot")
	result, err := RenderStringResult(root, `[docs][$id$]

![plot][$image$]

[$id$]: notes.md
[$image$]: plot.png`)
	if err != nil {
		t.Fatal(err)
	}
	wantLinks := []RenderLink{{Kind: LinkInternal, Target: "notes.md", Path: "notes.md"}}
	if !reflect.DeepEqual(result.Metadata.Links, wantLinks) {
		t.Fatalf("links = %#v, want %#v", result.Metadata.Links, wantLinks)
	}
	wantAssets := []RenderAsset{{Reference: "plot.png", Path: "plot.png", Status: AssetAvailable}}
	if !reflect.DeepEqual(result.Metadata.Assets, wantAssets) {
		t.Fatalf("assets = %#v, want %#v", result.Metadata.Assets, wantAssets)
	}
}

func TestRenderResultKeepsReferencesBeforeSameLineDisplayMath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "foo$bar.md"), "notes")
	writeFile(t, filepath.Join(root, "foo$bar.png"), "plot")
	result, err := RenderStringResult(root, `[docs](foo$bar.md) $$$z$$$

![plot](foo$bar.png) $$$w$$$`)
	if err != nil {
		t.Fatal(err)
	}
	wantLinks := []RenderLink{{Kind: LinkInternal, Target: "foo$bar.md", Path: "foo$bar.md"}}
	if !reflect.DeepEqual(result.Metadata.Links, wantLinks) {
		t.Fatalf("links = %#v, want %#v", result.Metadata.Links, wantLinks)
	}
	wantAssets := []RenderAsset{{Reference: "foo$bar.png", Path: "foo$bar.png", Status: AssetAvailable}}
	if !reflect.DeepEqual(result.Metadata.Assets, wantAssets) {
		t.Fatalf("assets = %#v, want %#v", result.Metadata.Assets, wantAssets)
	}
}

func TestRenderResultKeepsReferencesWithProtectedLabelDollars(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "foo$bar.md"), "notes")
	writeFile(t, filepath.Join(root, "foo$bar.png"), "plot")

	tests := []struct {
		name     string
		markdown string
	}{
		{name: "code span", markdown: "[docs `$`](foo$bar.md)\n\n![plot `$`](foo$bar.png)"},
		{name: "comment", markdown: "[docs <!-- $ -->](foo$bar.md)\n\n![plot <!-- $ -->](foo$bar.png)"},
		{name: "code element", markdown: "[docs <code>$</code>](foo$bar.md)\n\n![plot <code>$</code>](foo$bar.png)"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := RenderStringResult(root, test.markdown)
			if err != nil {
				t.Fatal(err)
			}
			wantLinks := []RenderLink{{Kind: LinkInternal, Target: "foo$bar.md", Path: "foo$bar.md"}}
			if !reflect.DeepEqual(result.Metadata.Links, wantLinks) {
				t.Fatalf("links = %#v, want %#v", result.Metadata.Links, wantLinks)
			}
			wantAssets := []RenderAsset{{Reference: "foo$bar.png", Path: "foo$bar.png", Status: AssetAvailable}}
			if !reflect.DeepEqual(result.Metadata.Assets, wantAssets) {
				t.Fatalf("assets = %#v, want %#v", result.Metadata.Assets, wantAssets)
			}
			if len(result.Metadata.Diagnostics) != 0 {
				t.Fatalf("unexpected diagnostics: %#v", result.Metadata.Diagnostics)
			}
		})
	}
}

func TestRenderResultExcludesReferencesInsideInlineMathSpanningDisplayMath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "notes.md"), "notes")
	writeFile(t, filepath.Join(root, "plot.png"), "plot")
	result, err := RenderStringResult(root, `$ [docs](notes.md) $$$z$$$ tail $

$ ![plot](plot.png) $$$w$$$ tail $`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Metadata.Links) != 0 || len(result.Metadata.Assets) != 0 || len(result.Metadata.Diagnostics) != 0 {
		t.Fatalf("unexpected metadata: links=%#v assets=%#v diagnostics=%#v", result.Metadata.Links, result.Metadata.Assets, result.Metadata.Diagnostics)
	}
}

func TestRenderResultTracksInlineMathClosedByDisplayContent(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "notes.md"), "notes")
	writeFile(t, filepath.Join(root, "plot.png"), "plot")
	result, err := RenderStringResult(root, `$ [docs](notes.md) $$$z $ $$$

$ ![plot](plot.png) $$$w $ $$$`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Metadata.Links) != 0 || len(result.Metadata.Assets) != 0 || len(result.Metadata.Diagnostics) != 0 {
		t.Fatalf("unexpected metadata: links=%#v assets=%#v diagnostics=%#v", result.Metadata.Links, result.Metadata.Assets, result.Metadata.Diagnostics)
	}
}

func TestRenderResultIgnoresInlineMathStartingInDisplayContent(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "notes.md"), "notes")
	writeFile(t, filepath.Join(root, "plot.png"), "plot")
	result, err := RenderStringResult(root, `$$$z $ $$$ [docs](notes.md) $y$

$$$w $ $$$ ![plot](plot.png) $v$`)
	if err != nil {
		t.Fatal(err)
	}
	wantLinks := []RenderLink{{Kind: LinkInternal, Target: "notes.md", Path: "notes.md"}}
	if !reflect.DeepEqual(result.Metadata.Links, wantLinks) {
		t.Fatalf("links = %#v, want %#v", result.Metadata.Links, wantLinks)
	}
	wantAssets := []RenderAsset{{Reference: "plot.png", Path: "plot.png", Status: AssetAvailable}}
	if !reflect.DeepEqual(result.Metadata.Assets, wantAssets) {
		t.Fatalf("assets = %#v, want %#v", result.Metadata.Assets, wantAssets)
	}
}

func TestRenderResultIgnoresCommentDollarsWhenFindingMath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "foo$bar.md"), "notes")
	writeFile(t, filepath.Join(root, "foo$bar.png"), "plot")
	result, err := RenderStringResult(root, `[docs](foo$bar.md) <!-- $ -->

![plot](foo$bar.png) <!-- $ -->`)
	if err != nil {
		t.Fatal(err)
	}
	wantLinks := []RenderLink{{Kind: LinkInternal, Target: "foo$bar.md", Path: "foo$bar.md"}}
	if !reflect.DeepEqual(result.Metadata.Links, wantLinks) {
		t.Fatalf("links = %#v, want %#v", result.Metadata.Links, wantLinks)
	}
	wantAssets := []RenderAsset{{Reference: "foo$bar.png", Path: "foo$bar.png", Status: AssetAvailable}}
	if !reflect.DeepEqual(result.Metadata.Assets, wantAssets) {
		t.Fatalf("assets = %#v, want %#v", result.Metadata.Assets, wantAssets)
	}
}

func TestRenderResultKeepsDollarsInEscapedCommentsWhenFindingMath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "notes.md"), "notes")
	writeFile(t, filepath.Join(root, "plot.png"), "plot")
	result, err := RenderStringResult(root, `$ [docs](notes.md) \<!-- $ -->

$ ![plot](plot.png) \<!-- $ -->`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Metadata.Links) != 0 || len(result.Metadata.Assets) != 0 || len(result.Metadata.Diagnostics) != 0 {
		t.Fatalf("unexpected metadata: links=%#v assets=%#v diagnostics=%#v", result.Metadata.Links, result.Metadata.Assets, result.Metadata.Diagnostics)
	}
}

func TestRenderResultCollectsNestedInlineReferences(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "docs.md"), "docs")
	writeFile(t, filepath.Join(root, "badge.svg"), "badge")
	result, err := RenderStringResult(root, `*[docs](docs.md)* [![badge](badge.svg)](https://example.com)`)
	if err != nil {
		t.Fatal(err)
	}
	wantLinks := []RenderLink{
		{Kind: LinkInternal, Target: "docs.md", Path: "docs.md"},
		{Kind: LinkExternal, Target: "https://example.com"},
	}
	if !reflect.DeepEqual(result.Metadata.Links, wantLinks) {
		t.Fatalf("links = %#v, want %#v", result.Metadata.Links, wantLinks)
	}
	wantAssets := []RenderAsset{{Reference: "badge.svg", Path: "badge.svg", Status: AssetAvailable}}
	if !reflect.DeepEqual(result.Metadata.Assets, wantAssets) {
		t.Fatalf("assets = %#v, want %#v", result.Metadata.Assets, wantAssets)
	}
}

func TestRenderResultSeparatesNestedReferenceMathRanges(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "badge.svg"), "badge")
	result, err := RenderStringResult(root, `[![badge](badge.svg)](https://example.com/foo$bar) tail $`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Metadata.Links) != 0 {
		t.Fatalf("unexpected links: %#v", result.Metadata.Links)
	}
	wantAssets := []RenderAsset{{Reference: "badge.svg", Path: "badge.svg", Status: AssetAvailable}}
	if !reflect.DeepEqual(result.Metadata.Assets, wantAssets) {
		t.Fatalf("assets = %#v, want %#v", result.Metadata.Assets, wantAssets)
	}
}

func TestRenderResultPreservesOuterLinkWhenNestedImageMathIsConsumed(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "page.md"), "page")
	writeFile(t, filepath.Join(root, "foo$bar.png"), "plot")
	result, err := RenderStringResult(root, `[![alt](foo$bar.png)](page.md) tail $`)
	if err != nil {
		t.Fatal(err)
	}
	wantLinks := []RenderLink{{Kind: LinkInternal, Target: "page.md", Path: "page.md"}}
	if !reflect.DeepEqual(result.Metadata.Links, wantLinks) {
		t.Fatalf("links = %#v, want %#v", result.Metadata.Links, wantLinks)
	}
	if len(result.Metadata.Assets) != 0 || len(result.Metadata.Diagnostics) != 0 {
		t.Fatalf("unexpected metadata: assets=%#v diagnostics=%#v", result.Metadata.Assets, result.Metadata.Diagnostics)
	}
}

func TestRenderResultExcludesAutolinksWhoseDollarIsDuplicated(t *testing.T) {
	result, err := RenderStringResult(t.TempDir(), `<https://example.com/foo$bar>`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Metadata.Links) != 0 {
		t.Fatalf("unexpected links: %#v", result.Metadata.Links)
	}
}

func TestRenderResultExcludesOuterLinkWhenHrefAndNestedImageContainDollars(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "page$baz.md"), "page")
	writeFile(t, filepath.Join(root, "foo$bar.png"), "plot")
	result, err := RenderStringResult(root, `[![alt](foo$bar.png)](page$baz.md)`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Metadata.Links) != 0 || len(result.Metadata.Assets) != 0 || len(result.Metadata.Diagnostics) != 0 {
		t.Fatalf("unexpected metadata: links=%#v assets=%#v diagnostics=%#v", result.Metadata.Links, result.Metadata.Assets, result.Metadata.Diagnostics)
	}
}

func TestRenderResultKeepsDollarReferencesSeparatedByRenderedNewlines(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "foo$bar.md"), "page")
	writeFile(t, filepath.Join(root, "foo$bar.png"), "plot")

	tests := []struct {
		name       string
		markdown   string
		wantLinks  []RenderLink
		wantAssets []RenderAsset
	}{
		{
			name:      "link label",
			markdown:  "[docs\n$](foo$bar.md)",
			wantLinks: []RenderLink{{Kind: LinkInternal, Target: "foo$bar.md", Path: "foo$bar.md"}},
		},
		{
			name:      "link title",
			markdown:  "[docs](foo$bar.md \"title\n$\")",
			wantLinks: []RenderLink{{Kind: LinkInternal, Target: "foo$bar.md", Path: "foo$bar.md"}},
		},
		{
			name:       "image label",
			markdown:   "![plot\n$](foo$bar.png)",
			wantAssets: []RenderAsset{{Reference: "foo$bar.png", Path: "foo$bar.png", Status: AssetAvailable}},
		},
		{
			name:       "image title",
			markdown:   "![plot](foo$bar.png \"title\n$\")",
			wantAssets: []RenderAsset{{Reference: "foo$bar.png", Path: "foo$bar.png", Status: AssetAvailable}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := RenderStringResult(root, test.markdown)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(result.Metadata.Links, test.wantLinks) {
				t.Fatalf("links = %#v, want %#v", result.Metadata.Links, test.wantLinks)
			}
			if !reflect.DeepEqual(result.Metadata.Assets, test.wantAssets) {
				t.Fatalf("assets = %#v, want %#v", result.Metadata.Assets, test.wantAssets)
			}
		})
	}
}

func TestRenderResultNormalizesMathDelimiters(t *testing.T) {
	result, err := RenderStringResult(t.TempDir(), `&#36;![numeric](numeric.png)$

\$![escaped](escaped.png)$`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Metadata.Assets) != 0 || len(result.Metadata.Diagnostics) != 0 {
		t.Fatalf("unexpected metadata: assets=%#v diagnostics=%#v", result.Metadata.Assets, result.Metadata.Diagnostics)
	}
}

func TestRenderResultPreservesEntitiesInsideRawHTMLWhenFindingMath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "x.png"), "x")
	result, err := RenderStringResult(root, `<span title="&#36;">x</span> ![x](x.png) $`)
	if err != nil {
		t.Fatal(err)
	}
	want := []RenderAsset{{Reference: "x.png", Path: "x.png", Status: AssetAvailable}}
	if !reflect.DeepEqual(result.Metadata.Assets, want) {
		t.Fatalf("assets = %#v, want %#v", result.Metadata.Assets, want)
	}
}

func TestRenderResultIgnoresCodeDollarsWhenFindingMath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "x.png"), "x")
	cases := []string{
		"`$` ![x](x.png) $",
		"```text\n$\n```\n\n![x](x.png) $",
		"```$$$\ncode\n```\n\n![x](x.png)\n\n$$$",
		"<code>$</code> ![x](x.png) $",
	}
	for _, markdown := range cases {
		result, err := RenderStringResult(root, markdown)
		if err != nil {
			t.Fatal(err)
		}
		want := []RenderAsset{{Reference: "x.png", Path: "x.png", Status: AssetAvailable}}
		if !reflect.DeepEqual(result.Metadata.Assets, want) {
			t.Fatalf("markdown %q: assets = %#v, want %#v", markdown, result.Metadata.Assets, want)
		}
	}
}

func TestRenderResultUsesMarpSlideReferenceBoundaries(t *testing.T) {
	result, err := RenderStringResult(t.TempDir(), `---
marp: true
---
[unresolved][id]

---

[id]: cross-slide.md

---

[resolved][local]

[local]: same-slide.md`)
	if err != nil {
		t.Fatal(err)
	}
	want := []RenderLink{{Kind: LinkInternal, Target: "same-slide.md", Path: "same-slide.md"}}
	if !reflect.DeepEqual(result.Metadata.Links, want) {
		t.Fatalf("links = %#v, want %#v", result.Metadata.Links, want)
	}
	if strings.Contains(result.HTML, "cross-slide.md") {
		t.Fatalf("cross-slide reference unexpectedly rendered: %s", result.HTML)
	}
}

func TestRenderResultDoesNotTreatEmptyAssetPathsAsEscapes(t *testing.T) {
	result, err := RenderStringResult(t.TempDir(), "![](#icon)\n\n![](?v=1)\n\n![]()")
	if err != nil {
		t.Fatal(err)
	}
	want := []RenderAsset{
		{Reference: "#icon", Status: AssetUnresolved},
		{Reference: "?v=1", Status: AssetUnresolved},
		{Reference: "", Status: AssetUnresolved},
	}
	if !reflect.DeepEqual(result.Metadata.Assets, want) {
		t.Fatalf("assets = %#v, want %#v", result.Metadata.Assets, want)
	}
	if len(result.Metadata.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", result.Metadata.Diagnostics)
	}
}

func TestRenderResultPreservesEmptyAltImageSourceOrder(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.png"), "a")
	writeFile(t, filepath.Join(root, "b.png"), "b")
	result, err := RenderStringResult(root, "`a.png`\n\n![](b.png)\n\n![](a.png)")
	if err != nil {
		t.Fatal(err)
	}
	want := []RenderAsset{
		{Reference: "b.png", Path: "b.png", Status: AssetAvailable},
		{Reference: "a.png", Path: "a.png", Status: AssetAvailable},
	}
	if !reflect.DeepEqual(result.Metadata.Assets, want) {
		t.Fatalf("assets = %#v, want %#v", result.Metadata.Assets, want)
	}
}

func TestRenderResultTreatsInvalidPercentEscapeAsLiteralLocalPath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "bad%zz.png"), "image")
	result, err := RenderStringResult(root, `![image](bad%zz.png)`)
	if err != nil {
		t.Fatal(err)
	}
	want := []RenderAsset{{Reference: "bad%zz.png", Path: "bad%zz.png", Status: AssetAvailable}}
	if !reflect.DeepEqual(result.Metadata.Assets, want) {
		t.Fatalf("assets = %#v, want %#v", result.Metadata.Assets, want)
	}
	if len(result.Metadata.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", result.Metadata.Diagnostics)
	}
}

func TestRenderResultNormalizesCommonMarkEscapesForLocalAssets(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "diagram(1).png"), "image")
	writeFile(t, filepath.Join(root, "a&b.png"), "image")
	result, err := RenderStringResult(root, `![escaped](diagram\(1\).png)

![entity](a&amp;b.png)`)
	if err != nil {
		t.Fatal(err)
	}
	want := []RenderAsset{
		{Reference: `diagram\(1\).png`, Path: "diagram(1).png", Status: AssetAvailable},
		{Reference: "a&amp;b.png", Path: "a&b.png", Status: AssetAvailable},
	}
	if !reflect.DeepEqual(result.Metadata.Assets, want) {
		t.Fatalf("assets = %#v, want %#v", result.Metadata.Assets, want)
	}
	if len(result.Metadata.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", result.Metadata.Diagnostics)
	}
}

func TestRenderResultNormalizesCommonMarkEntitiesForLinkKinds(t *testing.T) {
	result, err := RenderStringResult(t.TempDir(), `[fragment](&#35;section)

[email](mailto&#58;a@example.com)`)
	if err != nil {
		t.Fatal(err)
	}
	want := []RenderLink{
		{Kind: LinkFragment, Target: "&#35;section"},
		{Kind: LinkEmail, Target: "mailto&#58;a@example.com"},
	}
	if !reflect.DeepEqual(result.Metadata.Links, want) {
		t.Fatalf("links = %#v, want %#v", result.Metadata.Links, want)
	}
}

func TestRenderResultNormalizesCommonMarkEntitiesForDataAssets(t *testing.T) {
	result, err := RenderStringResult(t.TempDir(), `![embedded](data&#58;image/png;base64,AAAA)`)
	if err != nil {
		t.Fatal(err)
	}
	want := []RenderAsset{{Reference: "data&#58;image/png;base64,AAAA", Status: AssetEmbedded}}
	if !reflect.DeepEqual(result.Metadata.Assets, want) {
		t.Fatalf("assets = %#v, want %#v", result.Metadata.Assets, want)
	}
}

func TestRenderResultReportsAssetStatFailures(t *testing.T) {
	root := t.TempDir()
	renderer := NewRenderer(statFailureFileSystem{OSFileSystem: OSFileSystem{}})
	result, err := renderer.RenderStringResultWithOptions(root, `![locked](locked.png)`, false)
	if err != nil {
		t.Fatal(err)
	}
	wantAssets := []RenderAsset{{Reference: "locked.png", Path: "locked.png", Status: AssetUnavailable}}
	if !reflect.DeepEqual(result.Metadata.Assets, wantAssets) {
		t.Fatalf("assets = %#v, want %#v", result.Metadata.Assets, wantAssets)
	}
	wantDiagnostics := []RenderDiagnostic{{
		Severity: DiagnosticError,
		Code:     "asset_stat_failed",
		Message:  `could not inspect asset "locked.png": permission denied`,
		Path:     "locked.png",
	}}
	if !reflect.DeepEqual(result.Metadata.Diagnostics, wantDiagnostics) {
		t.Fatalf("diagnostics = %#v, want %#v", result.Metadata.Diagnostics, wantDiagnostics)
	}
}

type statFailureFileSystem struct {
	OSFileSystem
}

func (statFailureFileSystem) Stat(string) (fs.FileInfo, error) {
	return nil, errors.New("permission denied")
}

func TestLegacyRenderAPIStillReturnsFrontMatter(t *testing.T) {
	html, fm, err := RenderString(t.TempDir(), "---\ntitle: Legacy\n---\n# Body")
	if err != nil {
		t.Fatal(err)
	}
	if fm.Title != "Legacy" {
		t.Fatalf("unexpected front matter: %+v", fm)
	}
	assertContains(t, html, "<h1>Body</h1>")
	encoded, err := json.Marshal(fm)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"Title":"Legacy"`, `"Marp":false`, `"Theme":""`, `"Owners":null`, `"Data":{"title":"Legacy"}`} {
		if !strings.Contains(string(encoded), want) {
			t.Fatalf("legacy FrontMatter JSON does not contain %q: %s", want, encoded)
		}
	}
}
