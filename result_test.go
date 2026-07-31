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

func TestLegacyRenderAPIStillReturnsFrontMatter(t *testing.T) {
	html, fm, err := RenderString(t.TempDir(), "---\ntitle: Legacy\n---\n# Body")
	if err != nil {
		t.Fatal(err)
	}
	if fm.Title != "Legacy" {
		t.Fatalf("unexpected front matter: %+v", fm)
	}
	assertContains(t, html, "<h1>Body</h1>")
}
