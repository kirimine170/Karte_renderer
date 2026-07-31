package renderer

import (
	"encoding/json"
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
