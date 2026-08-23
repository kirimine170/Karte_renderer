package renderer

import (
	"context"
	"errors"
	"fmt"
	"html"
	"path/filepath"
	"strings"
	"sync"
)

// ErrPreviewerClosed is returned when Render is called after Close.
var ErrPreviewerClosed = errors.New("renderer previewer is closed")

// PreviewerOptions configures the process and filesystem used by a Previewer.
// The Node process is started lazily only when a Marp preview is requested.
type PreviewerOptions struct {
	FileSystem FileSystem
	NodePath   string
	HelperPath string
}

// Previewer owns the reusable official Marp Core bridge used by preview
// renders. Callers must call Close when the preview lifecycle ends.
type Previewer struct {
	renderer *Renderer
	bridge   *MarpBridge

	mu     sync.Mutex
	closed bool
}

// NewPreviewer constructs a context-aware preview renderer. At most one
// options value may be supplied.
func NewPreviewer(options ...PreviewerOptions) (*Previewer, error) {
	if len(options) > 1 {
		return nil, fmt.Errorf("NewPreviewer accepts at most one options value")
	}
	var opts PreviewerOptions
	if len(options) == 1 {
		opts = options[0]
	}
	r := NewRenderer(opts.FileSystem)
	return &Previewer{
		renderer: r,
		bridge: NewMarpBridge(MarpBridgeOptions{
			NodePath:   opts.NodePath,
			HelperPath: opts.HelperPath,
		}),
	}, nil
}

// Render creates a normal Markdown or official Marp Core preview. It is safe
// for concurrent callers. Cancelling ctx only cancels that caller; late bridge
// responses cannot be delivered to another request.
func (p *Previewer) Render(ctx context.Context, root, source string, opts PreviewOptions) (PreviewResult, error) {
	if p == nil {
		return PreviewResult{}, fmt.Errorf("nil Previewer")
	}
	if err := p.ensureOpen(); err != nil {
		return PreviewResult{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return PreviewResult{}, err
	}

	normalizedRoot, err := p.renderer.normalizeRoot(root)
	if err != nil {
		return PreviewResult{}, err
	}
	sourcePath, sourceFullPath, baseDir, err := normalizePreviewPath(normalizedRoot, opts.Path)
	if err != nil {
		return PreviewResult{}, err
	}
	body, frontMatter, err := parseFrontMatter(source)
	if err != nil {
		return PreviewResult{}, err
	}
	_, marpDeclared := frontMatter.Data["marp"]
	isMarp := frontMatter.Marp || (!marpDeclared && strings.HasSuffix(strings.ToLower(sourcePath), ".marp.md"))
	if !isMarp {
		return p.renderMarkdownPreview(normalizedRoot, sourcePath, sourceFullPath, baseDir, source, opts)
	}
	return p.renderMarpPreview(ctx, normalizedRoot, sourcePath, sourceFullPath, baseDir, source, body, frontMatter, opts)
}

// Close stops the owned Node process and rejects future Render calls. It is
// idempotent.
func (p *Previewer) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()
	return p.bridge.Close()
}

func (p *Previewer) ensureOpen() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrPreviewerClosed
	}
	return nil
}

func (p *Previewer) renderMarkdownPreview(root, path, sourceFullPath, baseDir, source string, opts PreviewOptions) (PreviewResult, error) {
	collector := newMetadataCollector(root, p.renderer.fs, p.renderer.usesOSFileSystem())
	if sourceFullPath != "" {
		collector.addDependency(DependencySource, sourceFullPath)
	}
	result, err := p.renderer.renderResult(root, baseDir, source, opts.HardWrap, defaultDocumentCSS, collector)
	if err != nil {
		return PreviewResult{}, err
	}
	result.HTML = insertBaseURL(result.HTML, baseDir)
	pageMode, pages, err := BuildMarkdownPreviewPages(path, source)
	if err != nil {
		return PreviewResult{}, err
	}
	return PreviewResult{
		SchemaVersion: PreviewSchemaVersion,
		Kind:          PreviewKindMarkdown,
		PageMode:      pageMode,
		HTML:          result.HTML,
		Pages:         pages,
		Metadata:      result.Metadata,
	}, nil
}

func (p *Previewer) renderMarpPreview(
	ctx context.Context,
	root, path, sourceFullPath, baseDir, source, body string,
	frontMatter FrontMatter,
	opts PreviewOptions,
) (PreviewResult, error) {
	collector := newMetadataCollector(root, p.renderer.fs, p.renderer.usesOSFileSystem())
	if sourceFullPath != "" {
		collector.addDependency(DependencySource, sourceFullPath)
	}
	expandedBody, err := p.renderer.expandImportsWithMetadata(root, baseDir, body, opts.HardWrap, collector)
	if err != nil {
		return PreviewResult{}, fmt.Errorf("prepare Marp preview: %w", err)
	}
	collector.collectReferences(baseDir, expandedBody)
	frontMatterPrefix := source[:len(source)-len(body)]
	renderSource := frontMatterPrefix + expandedBody
	mappingChanged := expandedBody != body

	response, err := p.bridge.Render(ctx, MarpRenderRequest{
		Root:        root,
		BaseDir:     baseDir,
		Source:      renderSource,
		Theme:       opts.Theme,
		ThemeSet:    opts.ThemeSet,
		TrustedHTML: opts.TrustedHTML,
	})
	if err != nil {
		return PreviewResult{}, fmt.Errorf("render Marp preview: %w", err)
	}
	boundaries, err := DetectMarpSectionBoundaries(renderSource)
	if err != nil {
		return PreviewResult{}, fmt.Errorf("map Marp preview pages: %w", err)
	}
	if mappingChanged {
		for i := range boundaries {
			boundaries[i].Unmapped = true
		}
	}
	pages, err := BuildMarpPreviewPages(path, renderSource, boundaries)
	if err != nil {
		return PreviewResult{}, fmt.Errorf("build Marp preview pages: %w", err)
	}
	annotatedHTML, err := AttachPreviewPageMetadata(response.HTML, pages)
	if err != nil {
		return PreviewResult{}, fmt.Errorf("annotate Marp preview pages: %w", err)
	}

	metadata := RenderMetadata{
		SchemaVersion: RenderMetadataSchemaVersion,
		FrontMatter:   frontMatter,
		Dependencies:  collector.dependencies,
		Links:         collector.links,
		Assets:        collector.assets,
		Diagnostics:   append([]RenderDiagnostic(nil), collector.diagnostics...),
	}
	var diagnostics []RenderDiagnostic
	if mappingChanged {
		diagnostic := RenderDiagnostic{
			Severity: DiagnosticWarning,
			Code:     "preview_source_unmapped",
			Message:  "Marp imports or generated directives changed the rendered source; imported pages are not directly editable",
			Path:     path,
		}
		diagnostics = append(diagnostics, diagnostic)
	}
	for _, warning := range response.Warnings {
		diagnostic := RenderDiagnostic{
			Severity: DiagnosticWarning,
			Code:     "marp_core_warning",
			Message:  warning,
			Path:     path,
		}
		diagnostics = append(diagnostics, diagnostic)
	}

	return PreviewResult{
		SchemaVersion: PreviewSchemaVersion,
		Kind:          PreviewKindMarp,
		PageMode:      PreviewPageSlides,
		HTML:          marpPreviewDocument(baseDir, frontMatter.Title, response.CSS, annotatedHTML),
		Pages:         pages,
		Metadata:      metadata,
		Diagnostics:   diagnostics,
	}, nil
}

func normalizePreviewPath(root, path string) (relative, full, baseDir string, err error) {
	if path == "" {
		return "", "", root, nil
	}
	full, err = safeJoin(root, filepath.FromSlash(path))
	if err != nil {
		return "", "", "", err
	}
	relative, err = filepath.Rel(root, full)
	if err != nil {
		return "", "", "", fmt.Errorf("locate preview source below root: %w", err)
	}
	return filepath.ToSlash(relative), full, filepath.Dir(full), nil
}

func marpPreviewDocument(baseDir, title, css, body string) string {
	// Prevent a CSS payload from terminating its style element. Custom themes
	// are trusted root-scoped files, but keeping the document structurally
	// closed also protects callers that persist PreviewResult.HTML.
	css = strings.ReplaceAll(css, "</", `<\/`)
	document := `<!doctype html><html><head><meta charset="utf-8">` +
		`<meta name="viewport" content="width=device-width,initial-scale=1">` +
		`<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data: file:; media-src data: file:; font-src data: file:; style-src 'unsafe-inline'; script-src 'none'; connect-src 'none'; frame-src 'none'">` +
		`<title>` + html.EscapeString(title) + `</title>` +
		`<style id="karte-marp-preview-style">` + css + `</style></head><body>` + body + `</body></html>`
	return insertBaseURL(document, baseDir)
}
