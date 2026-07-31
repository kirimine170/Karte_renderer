package renderer

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// RenderMetadataSchemaVersion identifies the stable shape and semantics of RenderMetadata.
const RenderMetadataSchemaVersion = 1

// RenderResult contains rendered HTML and machine-readable metadata discovered while rendering.
type RenderResult struct {
	HTML     string         `json:"html"`
	Metadata RenderMetadata `json:"metadata"`
}

// RenderMetadata describes the source metadata and resources that influenced a render.
type RenderMetadata struct {
	SchemaVersion int                `json:"schemaVersion"`
	FrontMatter   FrontMatter        `json:"frontMatter"`
	Dependencies  []RenderDependency `json:"dependencies,omitempty"`
	Links         []RenderLink       `json:"links,omitempty"`
	Assets        []RenderAsset      `json:"assets,omitempty"`
	Diagnostics   []RenderDiagnostic `json:"diagnostics,omitempty"`
}

// DependencyKind identifies how a file contributed to the rendered document.
type DependencyKind string

const (
	DependencySource         DependencyKind = "source"
	DependencyMarkdownImport DependencyKind = "markdown-import"
	DependencyCSVImport      DependencyKind = "csv-import"
	DependencyTeXImport      DependencyKind = "tex-import"
	DependencyLayout         DependencyKind = "layout"
)

// RenderDependency is a root-relative, slash-separated file dependency.
type RenderDependency struct {
	Kind DependencyKind `json:"kind"`
	Path string         `json:"path"`
}

// LinkKind identifies the destination class of a Markdown link.
type LinkKind string

const (
	LinkExternal LinkKind = "external"
	LinkInternal LinkKind = "internal"
	LinkFragment LinkKind = "fragment"
	LinkEmail    LinkKind = "email"
)

// RenderLink records a Markdown link in source order.
type RenderLink struct {
	Kind   LinkKind `json:"kind"`
	Target string   `json:"target"`
	Path   string   `json:"path,omitempty"`
}

// AssetStatus describes whether a referenced asset can be resolved locally.
type AssetStatus string

const (
	AssetAvailable   AssetStatus = "available"
	AssetMissing     AssetStatus = "missing"
	AssetExternal    AssetStatus = "external"
	AssetEmbedded    AssetStatus = "embedded"
	AssetUnresolved  AssetStatus = "unresolved"
	AssetOutsideRoot AssetStatus = "outside-root"
)

// RenderAsset records an image or other embedded Markdown asset in source order.
type RenderAsset struct {
	Reference string      `json:"reference"`
	Path      string      `json:"path,omitempty"`
	Status    AssetStatus `json:"status"`
}

// DiagnosticSeverity is the impact of a non-fatal render diagnostic.
type DiagnosticSeverity string

const (
	DiagnosticInfo    DiagnosticSeverity = "info"
	DiagnosticWarning DiagnosticSeverity = "warning"
	DiagnosticError   DiagnosticSeverity = "error"
)

// RenderDiagnostic describes a non-fatal problem found while collecting metadata.
type RenderDiagnostic struct {
	Severity DiagnosticSeverity `json:"severity"`
	Code     string             `json:"code"`
	Message  string             `json:"message"`
	Path     string             `json:"path,omitempty"`
}

// RenderMarkdownResult renders a Markdown file and returns structured metadata.
func RenderMarkdownResult(root, path string) (RenderResult, error) {
	return defaultRenderer.RenderMarkdownResultWithOptions(root, path, false)
}

// RenderMarkdownResultWithOptions renders a Markdown file with options and metadata.
func RenderMarkdownResultWithOptions(root, path string, hardwrap bool) (RenderResult, error) {
	return defaultRenderer.RenderMarkdownResultWithOptions(root, path, hardwrap)
}

// RenderStringResult renders Markdown text and returns structured metadata.
func RenderStringResult(root, markdown string) (RenderResult, error) {
	return defaultRenderer.RenderStringResultWithOptions(root, markdown, false)
}

// RenderStringResultWithOptions renders Markdown text with options and metadata.
func RenderStringResultWithOptions(root, markdown string, hardwrap bool) (RenderResult, error) {
	return defaultRenderer.RenderStringResultWithOptions(root, markdown, hardwrap)
}

// RenderMarkdownResultWithOptions is the Renderer-bound form of the metadata API.
func (r *Renderer) RenderMarkdownResultWithOptions(root, path string, hardwrap bool) (RenderResult, error) {
	return r.renderMarkdownResultWithCSS(root, path, hardwrap, defaultDocumentCSS)
}

type metadataCollector struct {
	root           string
	fs             FileSystem
	resolveSymlink bool
	dependencies   []RenderDependency
	links          []RenderLink
	assets         []RenderAsset
	diagnostics    []RenderDiagnostic
	dependencySeen map[string]bool
}

func newMetadataCollector(root string, fs FileSystem, resolveSymlink bool) *metadataCollector {
	return &metadataCollector{root: root, fs: fs, resolveSymlink: resolveSymlink, dependencySeen: map[string]bool{}}
}

func dependencyKindForImport(kind string) DependencyKind {
	switch kind {
	case "md", "markdown":
		return DependencyMarkdownImport
	case "csv":
		return DependencyCSVImport
	case "tex":
		return DependencyTeXImport
	default:
		return DependencyKind(kind + "-import")
	}
}

func (c *metadataCollector) addDependency(kind DependencyKind, path string) {
	rel := c.relativePath(path)
	key := string(kind) + "\x00" + rel
	if c.dependencySeen[key] {
		return
	}
	c.dependencySeen[key] = true
	c.dependencies = append(c.dependencies, RenderDependency{Kind: kind, Path: rel})
}

func (c *metadataCollector) collectReferences(baseDir, markdown string) {
	source := []byte(markdown)
	document := newMarkdown(false).Parser().Parse(text.NewReader(source))
	type referenceOccurrence struct {
		offset   int
		sequence int
		asset    bool
		target   string
	}
	var references []referenceOccurrence
	autolinkCursors := map[string]int{}
	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		sequence := len(references)
		switch node := node.(type) {
		case *ast.Image:
			target := string(node.Destination)
			fallback := []byte(target)
			if target == "" {
				fallback = []byte("![]()")
			}
			references = append(references, referenceOccurrence{offset: nodeSourceOffset(node, source, fallback), sequence: sequence, asset: true, target: target})
		case *ast.Link:
			target := string(node.Destination)
			references = append(references, referenceOccurrence{offset: nodeSourceOffset(node, source, []byte(target)), sequence: sequence, target: target})
		case *ast.AutoLink:
			target := string(node.URL(source))
			if node.AutoLinkType == ast.AutoLinkEmail && !strings.HasPrefix(strings.ToLower(target), "mailto:") {
				target = "mailto:" + target
			}
			label := node.Label(source)
			offset := nextSourceOccurrence(source, label, autolinkCursors[string(label)])
			if offset >= 0 {
				autolinkCursors[string(label)] = offset + len(label)
			} else {
				offset = len(source) + sequence
			}
			references = append(references, referenceOccurrence{offset: offset, sequence: sequence, target: target})
		}
		return ast.WalkContinue, nil
	})
	sort.SliceStable(references, func(i, j int) bool {
		if references[i].offset == references[j].offset {
			return references[i].sequence < references[j].sequence
		}
		return references[i].offset < references[j].offset
	})
	for _, reference := range references {
		if reference.asset {
			c.addAsset(baseDir, reference.target)
		} else {
			c.addLink(baseDir, reference.target)
		}
	}
}

func nodeSourceOffset(node ast.Node, source, fallback []byte) int {
	offset := len(source)
	_ = ast.Walk(node, func(child ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			if textNode, ok := child.(*ast.Text); ok && textNode.Segment.Start < offset {
				offset = textNode.Segment.Start
			}
		}
		return ast.WalkContinue, nil
	})
	if offset < len(source) {
		return offset
	}
	if len(fallback) > 0 {
		if fallbackOffset := bytes.Index(source, fallback); fallbackOffset >= 0 {
			return fallbackOffset
		}
	}
	return len(source)
}

func nextSourceOccurrence(source, value []byte, start int) int {
	if len(value) == 0 || start >= len(source) {
		return -1
	}
	offset := bytes.Index(source[start:], value)
	if offset < 0 {
		return -1
	}
	return start + offset
}

func (c *metadataCollector) addLink(baseDir, target string) {
	link := RenderLink{Target: target}
	switch {
	case strings.HasPrefix(target, "#"):
		link.Kind = LinkFragment
	case strings.HasPrefix(strings.ToLower(target), "mailto:"):
		link.Kind = LinkEmail
	case isExternalReference(target):
		link.Kind = LinkExternal
	default:
		link.Kind = LinkInternal
		if full, ok := c.resolveLocalReference(baseDir, target); ok {
			link.Path = c.relativePath(full)
		}
	}
	c.links = append(c.links, link)
}

func (c *metadataCollector) addAsset(baseDir, reference string) {
	asset := RenderAsset{Reference: reference}
	switch {
	case strings.HasPrefix(strings.ToLower(reference), "data:"):
		asset.Status = AssetEmbedded
	case isExternalReference(reference):
		asset.Status = AssetExternal
	case referenceHasNoPath(reference):
		asset.Status = AssetUnresolved
	default:
		full, within := c.resolveLocalReference(baseDir, reference)
		if !within {
			asset.Status = AssetOutsideRoot
			c.diagnostics = append(c.diagnostics, RenderDiagnostic{
				Severity: DiagnosticWarning,
				Code:     "asset_outside_root",
				Message:  fmt.Sprintf("asset reference %q resolves outside the project root", reference),
			})
			break
		}
		asset.Path = c.relativePath(full)
		if c.resolveSymlink {
			if resolved, err := resolveWithinRoot(c.root, full); err == nil {
				full = resolved
			} else if escapes, _ := missingPathEscapesRoot(c.root, full); escapes {
				asset.Status = AssetOutsideRoot
				c.diagnostics = append(c.diagnostics, RenderDiagnostic{
					Severity: DiagnosticWarning,
					Code:     "asset_outside_root",
					Message:  fmt.Sprintf("asset %q escapes the project root through a symlink", asset.Path),
					Path:     asset.Path,
				})
				break
			}
		}
		if _, err := c.fs.Stat(full); err != nil {
			asset.Status = AssetMissing
			c.diagnostics = append(c.diagnostics, RenderDiagnostic{
				Severity: DiagnosticWarning,
				Code:     "missing_asset",
				Message:  fmt.Sprintf("asset %q does not exist", asset.Path),
				Path:     asset.Path,
			})
		} else {
			asset.Status = AssetAvailable
		}
	}
	c.assets = append(c.assets, asset)
}

func (c *metadataCollector) resolveLocalReference(baseDir, reference string) (string, bool) {
	u, err := url.Parse(reference)
	if err != nil {
		return "", false
	}
	p, err := url.PathUnescape(u.Path)
	if err != nil || p == "" {
		return "", false
	}
	var full string
	if strings.HasPrefix(p, "/") {
		full = filepath.Join(c.root, filepath.FromSlash(strings.TrimPrefix(p, "/")))
	} else {
		full = filepath.Join(baseDir, filepath.FromSlash(p))
	}
	full = filepath.Clean(full)
	return full, isWithin(c.root, full)
}

func (c *metadataCollector) relativePath(path string) string {
	rel, err := filepath.Rel(c.root, path)
	if err != nil {
		return filepath.ToSlash(filepath.Clean(path))
	}
	return filepath.ToSlash(rel)
}

func isExternalReference(reference string) bool {
	u, err := url.Parse(reference)
	return err == nil && (u.Scheme != "" || u.Host != "")
}

func referenceHasNoPath(reference string) bool {
	u, err := url.Parse(reference)
	return err == nil && u.Path == ""
}

// missingPathEscapesRoot resolves the nearest existing ancestor so a missing
// leaf below an escaping symlink is not misclassified as an ordinary miss.
func missingPathEscapesRoot(root, full string) (bool, error) {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false, err
	}
	for candidate := full; ; candidate = filepath.Dir(candidate) {
		if _, err := os.Lstat(candidate); err == nil {
			resolved, err := filepath.EvalSymlinks(candidate)
			if err != nil {
				return false, err
			}
			return !isWithin(resolvedRoot, resolved), nil
		} else if !os.IsNotExist(err) {
			return false, err
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return false, nil
		}
	}
}
