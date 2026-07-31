package renderer

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	gmutil "github.com/yuin/goldmark/util"
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

// MarshalJSON keeps RenderMetadata's schema independent from FrontMatter's
// legacy JSON encoding, which existing callers may already persist.
func (m RenderMetadata) MarshalJSON() ([]byte, error) {
	type frontMatterJSON struct {
		Title   string                 `json:"title,omitempty"`
		Marp    bool                   `json:"marp,omitempty"`
		Theme   string                 `json:"theme,omitempty"`
		Layout  string                 `json:"layout,omitempty"`
		Owners  []string               `json:"owners,omitempty"`
		Viewers []string               `json:"viewers,omitempty"`
		Data    map[string]interface{} `json:"data,omitempty"`
	}
	type metadataJSON struct {
		SchemaVersion int                `json:"schemaVersion"`
		FrontMatter   frontMatterJSON    `json:"frontMatter"`
		Dependencies  []RenderDependency `json:"dependencies,omitempty"`
		Links         []RenderLink       `json:"links,omitempty"`
		Assets        []RenderAsset      `json:"assets,omitempty"`
		Diagnostics   []RenderDiagnostic `json:"diagnostics,omitempty"`
	}
	return json.Marshal(metadataJSON{
		SchemaVersion: m.SchemaVersion,
		FrontMatter: frontMatterJSON{
			Title:   m.FrontMatter.Title,
			Marp:    m.FrontMatter.Marp,
			Theme:   m.FrontMatter.Theme,
			Layout:  m.FrontMatter.Layout,
			Owners:  m.FrontMatter.Owners,
			Viewers: m.FrontMatter.Viewers,
			Data:    m.FrontMatter.Data,
		},
		Dependencies: m.Dependencies,
		Links:        m.Links,
		Assets:       m.Assets,
		Diagnostics:  m.Diagnostics,
	})
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
	AssetUnavailable AssetStatus = "unavailable"
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
	sourcePath     string
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
	if kind == DependencySource && c.sourcePath == "" {
		c.sourcePath = path
	}
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
	mathRanges := mathSourceRanges(source)
	type referenceOccurrence struct {
		offset               int
		sequence             int
		asset                bool
		target               string
		classificationTarget string
	}
	var references []referenceOccurrence
	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch node.(type) {
		case *ast.Image, *ast.Link, *ast.AutoLink:
			if sourceOffsetInRanges(referenceSourceOffset(node, len(source), len(references)), mathRanges) {
				return ast.WalkContinue, nil
			}
		}
		sequence := len(references)
		switch node := node.(type) {
		case *ast.Image:
			target := string(node.Destination)
			references = append(references, referenceOccurrence{offset: referenceSourceOffset(node, len(source), sequence), sequence: sequence, asset: true, target: target})
		case *ast.Link:
			target := string(node.Destination)
			references = append(references, referenceOccurrence{offset: referenceSourceOffset(node, len(source), sequence), sequence: sequence, target: target})
		case *ast.AutoLink:
			target := string(node.Label(source))
			classificationTarget := string(node.URL(source))
			if node.AutoLinkType == ast.AutoLinkEmail && !strings.HasPrefix(strings.ToLower(classificationTarget), "mailto:") {
				classificationTarget = "mailto:" + classificationTarget
			}
			offset := referenceSourceOffset(node, len(source), sequence)
			references = append(references, referenceOccurrence{
				offset:               offset,
				sequence:             sequence,
				target:               target,
				classificationTarget: classificationTarget,
			})
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
			c.addLink(baseDir, reference.target, reference.classificationTarget)
		}
	}
}

type sourceRange struct {
	start int
	end   int
}

func mathSourceRanges(source []byte) []sourceRange {
	displayMatches := katexDisplayRe.FindAllIndex(source, -1)
	ranges := make([]sourceRange, 0, len(displayMatches))
	for _, match := range displayMatches {
		ranges = append(ranges, sourceRange{start: match[0], end: match[1]})
	}
	for _, match := range katexInlineRe.FindAllIndex(source, -1) {
		if !sourceOffsetInRanges(match[0], ranges) {
			ranges = append(ranges, sourceRange{start: match[0], end: match[1]})
		}
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].start < ranges[j].start })
	return ranges
}

func sourceOffsetInRanges(offset int, ranges []sourceRange) bool {
	for _, sourceRange := range ranges {
		if offset < sourceRange.start {
			return false
		}
		if offset < sourceRange.end {
			return true
		}
	}
	return false
}

func referenceSourceOffset(node ast.Node, sourceLength, sequence int) int {
	if offset := node.Pos(); offset >= 0 {
		return offset
	}
	return sourceLength + sequence
}

func (c *metadataCollector) addLink(baseDir, target, classificationTarget string) {
	link := RenderLink{Target: target}
	if classificationTarget == "" {
		classificationTarget = target
	}
	normalizedTarget := normalizeMarkdownDestination(classificationTarget)
	switch {
	case strings.HasPrefix(normalizedTarget, "#"):
		link.Kind = LinkFragment
	case strings.HasPrefix(strings.ToLower(normalizedTarget), "mailto:"):
		link.Kind = LinkEmail
	case isExternalReference(normalizedTarget):
		link.Kind = LinkExternal
	default:
		link.Kind = LinkInternal
		if full, ok := c.resolveLocalReference(baseDir, target); ok && c.linkPathWithinRoot(full) {
			link.Path = c.relativePath(full)
		}
	}
	c.links = append(c.links, link)
}

func (c *metadataCollector) addAsset(baseDir, reference string) {
	asset := RenderAsset{Reference: reference}
	normalizedReference := normalizeMarkdownDestination(reference)
	switch {
	case strings.HasPrefix(strings.ToLower(normalizedReference), "data:"):
		asset.Status = AssetEmbedded
	case isExternalReference(normalizedReference):
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
		if _, err := c.fs.Stat(full); os.IsNotExist(err) {
			asset.Status = AssetMissing
			c.diagnostics = append(c.diagnostics, RenderDiagnostic{
				Severity: DiagnosticWarning,
				Code:     "missing_asset",
				Message:  fmt.Sprintf("asset %q does not exist", asset.Path),
				Path:     asset.Path,
			})
		} else if err != nil {
			asset.Status = AssetUnavailable
			c.diagnostics = append(c.diagnostics, RenderDiagnostic{
				Severity: DiagnosticError,
				Code:     "asset_stat_failed",
				Message:  fmt.Sprintf("could not inspect asset %q: %v", asset.Path, err),
				Path:     asset.Path,
			})
		} else {
			asset.Status = AssetAvailable
		}
	}
	c.assets = append(c.assets, asset)
}

func (c *metadataCollector) resolveLocalReference(baseDir, reference string) (string, bool) {
	p := localReferencePath(reference)
	if p == "" {
		normalizedReference := normalizeMarkdownDestination(reference)
		if (normalizedReference == "" || strings.HasPrefix(normalizedReference, "?")) && c.sourcePath != "" {
			return c.sourcePath, isWithin(c.root, c.sourcePath)
		}
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

func (c *metadataCollector) linkPathWithinRoot(full string) bool {
	if !c.resolveSymlink {
		return true
	}
	if _, err := resolveWithinRoot(c.root, full); err == nil {
		return true
	}
	escapes, err := missingPathEscapesRoot(c.root, full)
	return err == nil && !escapes
}

func (c *metadataCollector) relativePath(path string) string {
	rel, err := filepath.Rel(c.root, path)
	if err != nil {
		return filepath.ToSlash(filepath.Clean(path))
	}
	return filepath.ToSlash(rel)
}

func isExternalReference(reference string) bool {
	reference = normalizeMarkdownDestination(reference)
	if strings.HasPrefix(reference, "//") {
		return true
	}
	colon := strings.IndexByte(reference, ':')
	if colon <= 0 {
		return false
	}
	for i, char := range reference[:colon] {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (i > 0 && ((char >= '0' && char <= '9') || char == '+' || char == '-' || char == '.')) {
			continue
		}
		return false
	}
	return true
}

func referenceHasNoPath(reference string) bool {
	return localReferencePath(reference) == ""
}

func localReferencePath(reference string) string {
	reference = normalizeMarkdownDestination(reference)
	if end := strings.IndexAny(reference, "?#"); end >= 0 {
		reference = reference[:end]
	}
	if decoded, err := url.PathUnescape(reference); err == nil {
		return decoded
	}
	return reference
}

func normalizeMarkdownDestination(reference string) string {
	normalized := gmutil.UnescapePunctuations([]byte(reference))
	normalized = gmutil.ResolveNumericReferences(normalized)
	normalized = gmutil.ResolveEntityNames(normalized)
	return string(normalized)
}

// missingPathEscapesRoot resolves the nearest existing ancestor so a missing
// leaf below an escaping symlink is not misclassified as an ordinary miss.
func missingPathEscapesRoot(root, full string) (bool, error) {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false, err
	}
	return missingPathEscapesResolvedRoot(resolvedRoot, full, map[string]bool{})
}

func missingPathEscapesResolvedRoot(resolvedRoot, full string, seen map[string]bool) (bool, error) {
	for candidate := full; ; candidate = filepath.Dir(candidate) {
		if info, err := os.Lstat(candidate); err == nil {
			resolved, err := filepath.EvalSymlinks(candidate)
			if err == nil {
				return !isWithin(resolvedRoot, resolved), nil
			}
			if info.Mode()&os.ModeSymlink == 0 {
				return false, err
			}
			candidate = filepath.Clean(candidate)
			if seen[candidate] {
				return false, fmt.Errorf("symlink cycle at %s", candidate)
			}
			seen[candidate] = true
			target, err := os.Readlink(candidate)
			if err != nil {
				return false, err
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(candidate), target)
			}
			target = filepath.Clean(target)
			if !isWithin(resolvedRoot, target) {
				return true, nil
			}
			return missingPathEscapesResolvedRoot(resolvedRoot, target, seen)
		} else if !os.IsNotExist(err) {
			return false, err
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return false, nil
		}
	}
}
