package renderer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPreviewerRendersMarkdownWithoutStartingNode(t *testing.T) {
	root := t.TempDir()
	previewer, err := NewPreviewer(PreviewerOptions{
		NodePath:   filepath.Join(root, "missing-node"),
		HelperPath: filepath.Join(root, "missing-helper.mjs"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer previewer.Close()

	result, err := previewer.Render(context.Background(), root, "# Normal document\n", PreviewOptions{Path: "notes.md"})
	if err != nil {
		t.Fatalf("normal Markdown unexpectedly required Node: %v", err)
	}
	if result.Kind != PreviewKindMarkdown || result.PageMode != PreviewPageContinuous {
		t.Fatalf("unexpected Markdown preview contract: %+v", result)
	}
	if result.SchemaVersion != PreviewSchemaVersion || result.Metadata.SchemaVersion != RenderMetadataSchemaVersion {
		t.Fatalf("unexpected schema versions: preview=%d metadata=%d", result.SchemaVersion, result.Metadata.SchemaVersion)
	}
	if !strings.Contains(result.HTML, "<h1>Normal document</h1>") {
		t.Fatalf("normal Markdown HTML was not rendered: %s", result.HTML)
	}
}

func TestPreviewerResolvesMarkdownAssetsFromSourceDirectory(t *testing.T) {
	root := t.TempDir()
	slidesDir := filepath.Join(root, "slides")
	if err := os.MkdirAll(slidesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(slidesDir, "local.png"), []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	previewer, err := NewPreviewer(PreviewerOptions{
		NodePath:   filepath.Join(root, "missing-node"),
		HelperPath: filepath.Join(root, "missing-helper.mjs"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer previewer.Close()

	result, err := previewer.Render(context.Background(), root, "# Notes\n\n![local](local.png)\n", PreviewOptions{Path: "slides/notes.md"})
	if err != nil {
		t.Fatal(err)
	}
	canonicalSlidesDir, err := filepath.EvalSymlinks(slidesDir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.HTML, fileURL(canonicalSlidesDir+string(os.PathSeparator))) {
		t.Fatalf("source-relative Markdown asset base is missing:\n%s", result.HTML)
	}
	if len(result.Metadata.Assets) != 1 || result.Metadata.Assets[0].Status != AssetAvailable || result.Metadata.Assets[0].Path != "slides/local.png" {
		t.Fatalf("source-relative Markdown asset metadata = %#v", result.Metadata.Assets)
	}
	if len(result.Metadata.Dependencies) != 1 || result.Metadata.Dependencies[0].Path != "slides/notes.md" {
		t.Fatalf("Markdown source dependency = %#v", result.Metadata.Dependencies)
	}
}

func TestPreviewerRejectsRenderAfterClose(t *testing.T) {
	previewer, err := NewPreviewer()
	if err != nil {
		t.Fatal(err)
	}
	if err := previewer.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = previewer.Render(context.Background(), t.TempDir(), "# Closed", PreviewOptions{})
	if !errors.Is(err, ErrPreviewerClosed) {
		t.Fatalf("Render after Close error = %v", err)
	}
	if err := previewer.Close(); err != nil {
		t.Fatalf("second Close must be idempotent: %v", err)
	}
}

func TestPreviewerHonorsCancelledContextBeforeRender(t *testing.T) {
	previewer, err := NewPreviewer()
	if err != nil {
		t.Fatal(err)
	}
	defer previewer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = previewer.Render(ctx, t.TempDir(), "# Cancelled", PreviewOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Render error = %v", err)
	}
}

func TestPreviewerRejectsSourcePathOutsideRoot(t *testing.T) {
	previewer, err := NewPreviewer()
	if err != nil {
		t.Fatal(err)
	}
	defer previewer.Close()
	_, err = previewer.Render(context.Background(), t.TempDir(), "# Outside", PreviewOptions{Path: "../outside.md"})
	if err == nil || !strings.Contains(err.Error(), "escapes root") {
		t.Fatalf("outside source path error = %v", err)
	}
}

func TestPreviewerRendersOfficialMarpCoreContract(t *testing.T) {
	requireMarpPreviewIntegration(t)
	root := t.TempDir()
	previewer, err := NewPreviewer()
	if err != nil {
		t.Fatal(err)
	}
	defer previewer.Close()

	source := "---\nmarp: true\ntheme: gaia\npaginate: true\nheader: Header\nfooter: Footer\n---\n" +
		"<!-- _class: lead -->\n# 同じ🙂\n\n```text\n---\n```\n\n---\n\n# 同じ🙂\n\n<div id=\"unsafe\">raw</div>\n\n<!-- speaker note -->\n"
	result, err := previewer.Render(context.Background(), root, source, PreviewOptions{Path: "deck.marp.md"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != PreviewKindMarp || result.PageMode != PreviewPageSlides || len(result.Pages) != 2 {
		t.Fatalf("unexpected Marp preview contract: kind=%q mode=%q pages=%d", result.Kind, result.PageMode, len(result.Pages))
	}
	if result.Pages[0].ID == result.Pages[1].ID {
		t.Fatalf("duplicate headings produced duplicate page IDs: %#v", result.Pages)
	}
	for index, page := range result.Pages {
		if page.Index != index || !page.Editable || page.Source == nil {
			t.Fatalf("page %d is not mapped: %#v", index, page)
		}
		if page.Source.StartByte < 0 || page.Source.EndByte > len(source) || page.Source.StartByte >= page.Source.EndByte {
			t.Fatalf("invalid page %d source range: %#v", index, page.Source)
		}
	}
	for _, want := range []string{
		`data-marpit-svg`,
		`data-karte-page-id=`,
		`data-karte-page-index=`,
		`karte-marp-preview-style`,
		`Content-Security-Policy`,
		`<header>Header</header>`,
		`<footer>Footer</footer>`,
		`hljs`,
	} {
		if !strings.Contains(result.HTML, want) {
			t.Fatalf("official Marp preview is missing %q:\n%s", want, result.HTML)
		}
	}
	if strings.Contains(result.HTML, `<div id="unsafe">raw</div>`) {
		t.Fatalf("raw HTML was enabled by default:\n%s", result.HTML)
	}
}

func TestPreviewerDetectsMarpSuffixAndExplicitFalse(t *testing.T) {
	requireMarpPreviewIntegration(t)
	root := t.TempDir()
	previewer, err := NewPreviewer()
	if err != nil {
		t.Fatal(err)
	}
	defer previewer.Close()

	marp, err := previewer.Render(context.Background(), root, "# Marp by path\n", PreviewOptions{Path: "deck.marp.md"})
	if err != nil {
		t.Fatal(err)
	}
	if marp.Kind != PreviewKindMarp {
		t.Fatalf(".marp.md kind = %q", marp.Kind)
	}
	markdown, err := previewer.Render(context.Background(), root, "---\nmarp: false\n---\n# Explicit Markdown\n", PreviewOptions{Path: "deck.marp.md"})
	if err != nil {
		t.Fatal(err)
	}
	if markdown.Kind != PreviewKindMarkdown {
		t.Fatalf("explicit marp:false kind = %q", markdown.Kind)
	}
}

func TestPreviewerResolvesAssetsFromSourceDirectory(t *testing.T) {
	requireMarpPreviewIntegration(t)
	root := t.TempDir()
	slidesDir := filepath.Join(root, "slides")
	if err := os.MkdirAll(slidesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(slidesDir, "local.png"), []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	previewer, err := NewPreviewer()
	if err != nil {
		t.Fatal(err)
	}
	defer previewer.Close()
	source := "---\nmarp: true\n---\n# Asset\n\n![local](local.png)\n"
	result, err := previewer.Render(context.Background(), root, source, PreviewOptions{Path: "slides/deck.marp.md"})
	if err != nil {
		t.Fatal(err)
	}
	canonicalSlidesDir, err := filepath.EvalSymlinks(slidesDir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.HTML, `src="local.png"`) || !strings.Contains(result.HTML, fileURL(canonicalSlidesDir+string(os.PathSeparator))) {
		t.Fatalf("source-relative asset base is missing:\n%s", result.HTML)
	}
}

func TestPreviewerAllowsRawHTMLOnlyWhenTrusted(t *testing.T) {
	requireMarpPreviewIntegration(t)
	previewer, err := NewPreviewer()
	if err != nil {
		t.Fatal(err)
	}
	defer previewer.Close()
	source := "---\nmarp: true\n---\n<span id=\"trusted\">trusted</span>\n"
	result, err := previewer.Render(context.Background(), t.TempDir(), source, PreviewOptions{TrustedHTML: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.HTML, `<span id="trusted">trusted</span>`) {
		t.Fatalf("trusted HTML was not rendered:\n%s", result.HTML)
	}
	if !strings.Contains(result.HTML, `script-src 'none'`) {
		t.Fatalf("trusted preview lost the no-script CSP:\n%s", result.HTML)
	}
}

func TestPreviewerMarksExpandedImportsUneditable(t *testing.T) {
	requireMarpPreviewIntegration(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "partial.md"), []byte("# Imported\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	previewer, err := NewPreviewer()
	if err != nil {
		t.Fatal(err)
	}
	defer previewer.Close()
	source := "---\nmarp: true\n---\n@import(type=\"md\" path=\"partial.md\")\n"
	result, err := previewer.Render(context.Background(), root, source, PreviewOptions{Path: "deck.marp.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Pages) != 1 || result.Pages[0].Editable || result.Pages[0].Source != nil {
		t.Fatalf("expanded import was exposed as editable source: %#v", result.Pages)
	}
	found := false
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "preview_source_unmapped" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing source mapping diagnostic: %#v", result.Diagnostics)
	}
}

func TestMarpPreviewPerformanceReport(t *testing.T) {
	requireMarpPreviewIntegration(t)
	if os.Getenv("KARTE_PREVIEW_PERF") != "1" {
		t.Skip("set KARTE_PREVIEW_PERF=1 to record cold and warm preview timings")
	}
	var source strings.Builder
	source.WriteString("---\nmarp: true\ntheme: default\npaginate: true\n---\n")
	for i := 0; i < 16; i++ {
		if i > 0 {
			source.WriteString("\n---\n")
		}
		fmt.Fprintf(&source, "\n# Slide %02d 日本語🙂\n\n", i+1)
		if i%3 == 0 {
			source.WriteString("```go\nfmt.Println(\"preview\")\n```\n")
		}
		if i%4 == 0 {
			source.WriteString("\n$$x^2 + y^2$$\n")
		}
	}
	previewer, err := NewPreviewer()
	if err != nil {
		t.Fatal(err)
	}
	defer previewer.Close()

	started := time.Now()
	result, err := previewer.Render(context.Background(), t.TempDir(), source.String(), PreviewOptions{Path: "performance.marp.md"})
	if err != nil {
		t.Fatal(err)
	}
	cold := time.Since(started)
	if len(result.Pages) != 16 {
		t.Fatalf("performance fixture pages = %d", len(result.Pages))
	}
	const warmRuns = 5
	started = time.Now()
	for i := 0; i < warmRuns; i++ {
		if _, err := previewer.Render(context.Background(), t.TempDir(), source.String(), PreviewOptions{Path: "performance.marp.md"}); err != nil {
			t.Fatal(err)
		}
	}
	warmAverage := time.Since(started) / warmRuns
	t.Logf("official Marp Core preview performance: slides=16 cold=%s warm-average=%s warm-runs=%d", cold, warmAverage, warmRuns)
}

func requireMarpPreviewIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv("KARTE_PREVIEW_INTEGRATION") != "1" {
		t.Skip("set KARTE_PREVIEW_INTEGRATION=1 after npm ci to run official Marp Core integration")
	}
}
