package renderer

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// FormatPathKind is the filesystem type required by a package path.
type FormatPathKind string

const (
	FormatPathFile      FormatPathKind = "file"
	FormatPathDirectory FormatPathKind = "directory"
)

// FormatResolverErrorCode is a stable diagnostic category for resolver failures.
type FormatResolverErrorCode string

const (
	FormatResolverInvalidPath       FormatResolverErrorCode = "invalid_path"
	FormatResolverAbsolutePath      FormatResolverErrorCode = "absolute_path"
	FormatResolverURIScheme         FormatResolverErrorCode = "uri_scheme"
	FormatResolverOutsidePackage    FormatResolverErrorCode = "outside_package"
	FormatResolverOutsideAssets     FormatResolverErrorCode = "outside_assets_directory"
	FormatResolverSymlinkEscape     FormatResolverErrorCode = "symlink_escape"
	FormatResolverPathNotFound      FormatResolverErrorCode = "path_not_found"
	FormatResolverExpectedFile      FormatResolverErrorCode = "expected_file"
	FormatResolverExpectedDirectory FormatResolverErrorCode = "expected_directory"
)

// FormatResolverError describes a path failure without requiring callers to
// parse an operating-system-specific error string.
type FormatResolverError struct {
	Code      FormatResolverErrorCode
	Source    string
	Reference string
	Err       error
}

func (e *FormatResolverError) Error() string {
	location := fmt.Sprintf("format package path %q", e.Reference)
	if e.Source != "" {
		location = fmt.Sprintf("format asset %q referenced by %q", e.Reference, e.Source)
	}
	if e.Err == nil {
		return fmt.Sprintf("%s: %s", location, e.Code)
	}
	return fmt.Sprintf("%s: %s: %v", location, e.Code, e.Err)
}

func (e *FormatResolverError) Unwrap() error { return e.Err }

// FormatResolvedPath is a validated package member. PackagePath always uses
// forward slashes; Path is the canonical host filesystem path.
type FormatResolvedPath struct {
	PackagePath string         `json:"packagePath"`
	Path        string         `json:"path"`
	Kind        FormatPathKind `json:"kind"`
}

// FormatAssetDisposition determines whether a CSS URL needs a distributed file.
type FormatAssetDisposition string

const (
	FormatAssetLocal    FormatAssetDisposition = "local"
	FormatAssetEmbedded FormatAssetDisposition = "embedded"
)

// FormatAssetKind identifies the two asset classes covered by the v0.1 theme
// contract.
type FormatAssetKind string

const (
	FormatAssetImage FormatAssetKind = "image"
	FormatAssetFont  FormatAssetKind = "font"
)

// FormatAssetReference records one url(...) occurrence in source order.
type FormatAssetReference struct {
	Source      string                 `json:"source"`
	Reference   string                 `json:"reference"`
	PackagePath string                 `json:"packagePath,omitempty"`
	Path        string                 `json:"path,omitempty"`
	Kind        FormatAssetKind        `json:"kind"`
	Disposition FormatAssetDisposition `json:"disposition"`
}

// FormatPackageAssets is the filesystem-validated manifest and the assets
// discovered from its Markdown styles and Marp themes.
type FormatPackageAssets struct {
	Root            string                 `json:"root"`
	Manifest        FormatManifest         `json:"manifest"`
	ManifestFile    FormatResolvedPath     `json:"manifestFile"`
	MarkdownLayout  FormatResolvedPath     `json:"markdownLayout"`
	MarkdownStyles  []FormatResolvedPath   `json:"markdownStyles"`
	MarpThemes      []FormatResolvedPath   `json:"marpThemes"`
	AssetsDirectory FormatResolvedPath     `json:"assetsDirectory"`
	References      []FormatAssetReference `json:"references"`
	Distribution    []FormatResolvedPath   `json:"distribution"`
}

// FormatAssetResolver resolves package members against one canonical package
// root. It intentionally does not apply a package to a conversion pipeline.
type FormatAssetResolver struct {
	root string
}

// NewFormatAssetResolver validates and canonicalizes a local package root.
func NewFormatAssetResolver(packageRoot string) (*FormatAssetResolver, error) {
	if strings.TrimSpace(packageRoot) == "" {
		return nil, &FormatResolverError{Code: FormatResolverInvalidPath, Reference: packageRoot, Err: errors.New("package root is required")}
	}
	absolute, err := filepath.Abs(packageRoot)
	if err != nil {
		return nil, &FormatResolverError{Code: FormatResolverInvalidPath, Reference: packageRoot, Err: err}
	}
	root, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		code := FormatResolverInvalidPath
		if os.IsNotExist(err) {
			code = FormatResolverPathNotFound
		}
		return nil, &FormatResolverError{Code: code, Reference: packageRoot, Err: err}
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, &FormatResolverError{Code: FormatResolverInvalidPath, Reference: packageRoot, Err: err}
	}
	if !info.IsDir() {
		return nil, &FormatResolverError{Code: FormatResolverExpectedDirectory, Reference: packageRoot, Err: errors.New("package root is not a directory")}
	}
	return &FormatAssetResolver{root: filepath.Clean(root)}, nil
}

// Root returns the canonical host path used as the security boundary.
func (r *FormatAssetResolver) Root() string { return r.root }

// Resolve validates a manifest-style package-root-relative path and its type.
func (r *FormatAssetResolver) Resolve(packagePath string, kind FormatPathKind) (FormatResolvedPath, error) {
	if code, err := validateResolverPackagePath(packagePath); err != nil {
		return FormatResolvedPath{}, &FormatResolverError{Code: code, Reference: packagePath, Err: err}
	}
	return r.resolveCanonicalPackagePath(packagePath, kind, "", packagePath)
}

// ResolveCSSReference resolves a CSS url(...) value relative to its declaring
// stylesheet. data: URLs and fragment-only references are embedded and do not
// produce a filesystem path.
func (r *FormatAssetResolver) ResolveCSSReference(stylesheetPackagePath, reference string, font bool) (FormatAssetReference, error) {
	if code, err := validateResolverPackagePath(stylesheetPackagePath); err != nil {
		return FormatAssetReference{}, &FormatResolverError{Code: code, Reference: stylesheetPackagePath, Err: err}
	}
	asset := FormatAssetReference{
		Source:      stylesheetPackagePath,
		Reference:   reference,
		Kind:        FormatAssetImage,
		Disposition: FormatAssetLocal,
	}
	if font {
		asset.Kind = FormatAssetFont
	}
	if strings.TrimSpace(reference) == "" {
		return FormatAssetReference{}, &FormatResolverError{Code: FormatResolverInvalidPath, Source: stylesheetPackagePath, Reference: reference, Err: errors.New("empty CSS URL")}
	}
	lower := strings.ToLower(reference)
	if strings.HasPrefix(reference, "#") {
		asset.Disposition = FormatAssetEmbedded
		return asset, nil
	}
	if strings.HasPrefix(lower, "data:") {
		asset.Disposition = FormatAssetEmbedded
		if strings.HasPrefix(lower, "data:font/") || strings.HasPrefix(lower, "data:application/font-") {
			asset.Kind = FormatAssetFont
		}
		return asset, nil
	}

	local := reference
	if end := strings.IndexAny(local, "?#"); end >= 0 {
		local = local[:end]
	}
	decoded, err := url.PathUnescape(local)
	if err != nil {
		return FormatAssetReference{}, &FormatResolverError{Code: FormatResolverInvalidPath, Source: stylesheetPackagePath, Reference: reference, Err: fmt.Errorf("decode CSS URL: %w", err)}
	}
	if strings.ContainsRune(decoded, '\x00') {
		return FormatAssetReference{}, &FormatResolverError{Code: FormatResolverInvalidPath, Source: stylesheetPackagePath, Reference: reference, Err: errors.New("path contains a NUL byte")}
	}
	if hasWindowsDrivePrefix(decoded) || strings.HasPrefix(decoded, "/") || strings.HasPrefix(decoded, `\\`) {
		return FormatAssetReference{}, &FormatResolverError{Code: FormatResolverAbsolutePath, Source: stylesheetPackagePath, Reference: reference, Err: errors.New("absolute CSS URLs are not package assets")}
	}
	if formatURISchemeRe.MatchString(decoded) {
		return FormatAssetReference{}, &FormatResolverError{Code: FormatResolverURIScheme, Source: stylesheetPackagePath, Reference: reference, Err: errors.New("URI schemes are not local package assets")}
	}
	if strings.Contains(decoded, "\\") {
		return FormatAssetReference{}, &FormatResolverError{Code: FormatResolverInvalidPath, Source: stylesheetPackagePath, Reference: reference, Err: errors.New("CSS asset paths must use forward slashes")}
	}
	if decoded == "" {
		return FormatAssetReference{}, &FormatResolverError{Code: FormatResolverInvalidPath, Source: stylesheetPackagePath, Reference: reference, Err: errors.New("CSS URL has no local path")}
	}

	packagePath := path.Clean(path.Join(path.Dir(stylesheetPackagePath), decoded))
	if packagePath == "." || packagePath == ".." || strings.HasPrefix(packagePath, "../") {
		return FormatAssetReference{}, &FormatResolverError{Code: FormatResolverOutsidePackage, Source: stylesheetPackagePath, Reference: reference, Err: errors.New("path escapes the format package")}
	}
	resolved, err := r.resolveCanonicalPackagePath(packagePath, FormatPathFile, stylesheetPackagePath, reference)
	if err != nil {
		return FormatAssetReference{}, err
	}
	asset.PackagePath = resolved.PackagePath
	asset.Path = resolved.Path
	if !font && isFontAssetPath(packagePath) {
		asset.Kind = FormatAssetFont
	}
	return asset, nil
}

// ResolveFormatPackageAssets loads a manifest, validates every declared
// filesystem member, and discovers url(...) references in stylesheet order.
func ResolveFormatPackageAssets(packageRoot string) (FormatPackageAssets, error) {
	resolver, err := NewFormatAssetResolver(packageRoot)
	if err != nil {
		return FormatPackageAssets{}, err
	}
	manifestFile, err := resolver.Resolve(FormatManifestFilename, FormatPathFile)
	if err != nil {
		return FormatPackageAssets{}, fmt.Errorf("resolve format manifest: %w", err)
	}
	manifestSource, err := os.ReadFile(manifestFile.Path)
	if err != nil {
		return FormatPackageAssets{}, fmt.Errorf("read format manifest %s: %w", manifestFile.Path, err)
	}
	manifest, err := ParseFormatManifest(manifestSource)
	if err != nil {
		return FormatPackageAssets{}, fmt.Errorf("parse format manifest %s: %w", manifestFile.Path, err)
	}
	result := FormatPackageAssets{Root: resolver.root, Manifest: manifest, ManifestFile: manifestFile}
	if result.MarkdownLayout, err = resolver.Resolve(manifest.Markdown.Layout, FormatPathFile); err != nil {
		return FormatPackageAssets{}, fmt.Errorf("resolve markdown.layout: %w", err)
	}
	for i, value := range manifest.Markdown.Styles {
		resolved, resolveErr := resolver.Resolve(value, FormatPathFile)
		if resolveErr != nil {
			return FormatPackageAssets{}, fmt.Errorf("resolve markdown.styles[%d]: %w", i, resolveErr)
		}
		result.MarkdownStyles = append(result.MarkdownStyles, resolved)
	}
	for i, value := range manifest.Marp.Themes {
		resolved, resolveErr := resolver.Resolve(value, FormatPathFile)
		if resolveErr != nil {
			return FormatPackageAssets{}, fmt.Errorf("resolve marp.themes[%d]: %w", i, resolveErr)
		}
		result.MarpThemes = append(result.MarpThemes, resolved)
	}
	if result.AssetsDirectory, err = resolver.Resolve(manifest.Assets.Directory, FormatPathDirectory); err != nil {
		return FormatPackageAssets{}, fmt.Errorf("resolve assets.directory: %w", err)
	}

	stylesheets := append(append([]FormatResolvedPath{}, result.MarkdownStyles...), result.MarpThemes...)
	for _, stylesheet := range stylesheets {
		references, discoverErr := discoverFormatCSSURLs(stylesheet.Path)
		if discoverErr != nil {
			return FormatPackageAssets{}, fmt.Errorf("discover format assets in %s: %w", stylesheet.PackagePath, discoverErr)
		}
		for _, reference := range references {
			asset, resolveErr := resolver.ResolveCSSReference(stylesheet.PackagePath, reference.value, reference.font)
			if resolveErr != nil {
				return FormatPackageAssets{}, resolveErr
			}
			if asset.Disposition == FormatAssetLocal && (!packagePathWithin(result.AssetsDirectory.PackagePath, asset.PackagePath) || !isWithin(result.AssetsDirectory.Path, asset.Path)) {
				return FormatPackageAssets{}, &FormatResolverError{
					Code:      FormatResolverOutsideAssets,
					Source:    stylesheet.PackagePath,
					Reference: reference.value,
					Err:       fmt.Errorf("local theme assets must be below assets.directory %q", result.AssetsDirectory.PackagePath),
				}
			}
			result.References = append(result.References, asset)
		}
	}
	result.Distribution = formatDistributionFiles(result)
	return result, nil
}

func formatDistributionFiles(result FormatPackageAssets) []FormatResolvedPath {
	files := []FormatResolvedPath{result.ManifestFile, result.MarkdownLayout}
	files = append(files, result.MarkdownStyles...)
	files = append(files, result.MarpThemes...)
	seen := make(map[string]bool, len(files)+len(result.References))
	unique := files[:0]
	for _, file := range files {
		if !seen[file.PackagePath] {
			seen[file.PackagePath] = true
			unique = append(unique, file)
		}
	}
	for _, reference := range result.References {
		if reference.Disposition != FormatAssetLocal || seen[reference.PackagePath] {
			continue
		}
		seen[reference.PackagePath] = true
		unique = append(unique, FormatResolvedPath{PackagePath: reference.PackagePath, Path: reference.Path, Kind: FormatPathFile})
	}
	return unique
}

func validateResolverPackagePath(value string) (FormatResolverErrorCode, error) {
	if hasWindowsDrivePrefix(value) || path.IsAbs(value) || strings.HasPrefix(value, `\\`) {
		return FormatResolverAbsolutePath, errors.New("path must be package-root-relative")
	}
	if formatURISchemeRe.MatchString(value) {
		return FormatResolverURIScheme, errors.New("URI schemes are not package paths")
	}
	if err := validateFormatPath("path", value); err != nil {
		code := FormatResolverInvalidPath
		cleaned := path.Clean(value)
		if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
			code = FormatResolverOutsidePackage
		}
		return code, err
	}
	return "", nil
}

func (r *FormatAssetResolver) resolveCanonicalPackagePath(packagePath string, kind FormatPathKind, source, reference string) (FormatResolvedPath, error) {
	if kind != FormatPathFile && kind != FormatPathDirectory {
		return FormatResolvedPath{}, &FormatResolverError{Code: FormatResolverInvalidPath, Source: source, Reference: reference, Err: fmt.Errorf("unsupported path kind %q", kind)}
	}
	full := filepath.Join(r.root, filepath.FromSlash(packagePath))
	if !isWithin(r.root, full) {
		return FormatResolvedPath{}, &FormatResolverError{Code: FormatResolverOutsidePackage, Source: source, Reference: reference, Err: errors.New("path escapes the format package")}
	}
	resolved, err := filepath.EvalSymlinks(full)
	if err != nil {
		if os.IsNotExist(err) {
			if escapes, escapeErr := missingPathEscapesRoot(r.root, full); escapeErr == nil && escapes {
				return FormatResolvedPath{}, &FormatResolverError{Code: FormatResolverSymlinkEscape, Source: source, Reference: reference, Err: errors.New("path escapes the format package through a symlink")}
			}
			return FormatResolvedPath{}, &FormatResolverError{Code: FormatResolverPathNotFound, Source: source, Reference: reference, Err: err}
		}
		return FormatResolvedPath{}, &FormatResolverError{Code: FormatResolverInvalidPath, Source: source, Reference: reference, Err: err}
	}
	if !isWithin(r.root, resolved) {
		return FormatResolvedPath{}, &FormatResolverError{Code: FormatResolverSymlinkEscape, Source: source, Reference: reference, Err: errors.New("path escapes the format package through a symlink")}
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return FormatResolvedPath{}, &FormatResolverError{Code: FormatResolverInvalidPath, Source: source, Reference: reference, Err: err}
	}
	if kind == FormatPathFile && !info.Mode().IsRegular() {
		return FormatResolvedPath{}, &FormatResolverError{Code: FormatResolverExpectedFile, Source: source, Reference: reference, Err: errors.New("resolved path is not a regular file")}
	}
	if kind == FormatPathDirectory && !info.IsDir() {
		return FormatResolvedPath{}, &FormatResolverError{Code: FormatResolverExpectedDirectory, Source: source, Reference: reference, Err: errors.New("resolved path is not a directory")}
	}
	return FormatResolvedPath{PackagePath: packagePath, Path: filepath.Clean(resolved), Kind: kind}, nil
}

func packagePathWithin(directory, file string) bool {
	return file != directory && strings.HasPrefix(file, strings.TrimSuffix(directory, "/")+"/")
}

func isFontAssetPath(value string) bool {
	switch strings.ToLower(path.Ext(value)) {
	case ".woff", ".woff2", ".ttf", ".otf", ".eot":
		return true
	default:
		return false
	}
}

type discoveredFormatCSSURL struct {
	value string
	font  bool
}

func discoverFormatCSSURLs(filename string) ([]discoveredFormatCSSURL, error) {
	source, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	css := string(source)
	var references []discoveredFormatCSSURL
	var blocks []bool
	pendingFontFace := false
	pendingImport := false
	for i := 0; i < len(css); {
		switch {
		case strings.HasPrefix(css[i:], "/*"):
			end := strings.Index(css[i+2:], "*/")
			if end < 0 {
				return nil, errors.New("unterminated CSS comment")
			}
			i += end + 4
		case css[i] == '\'' || css[i] == '"':
			end, scanErr := scanCSSString(css, i)
			if scanErr != nil {
				return nil, scanErr
			}
			i = end
		case css[i] == '@' && cssNameAt(css, i+1, "font-face"):
			pendingFontFace = true
			i += len("@font-face")
		case css[i] == '@' && cssNameAt(css, i+1, "import"):
			pendingImport = true
			i += len("@import")
		case css[i] == '{':
			blocks = append(blocks, pendingFontFace)
			pendingFontFace = false
			pendingImport = false
			i++
		case css[i] == '}':
			if len(blocks) > 0 {
				blocks = blocks[:len(blocks)-1]
			}
			pendingFontFace = false
			i++
		case css[i] == ';':
			pendingFontFace = false
			pendingImport = false
			i++
		case cssNameAt(css, i, "url"):
			value, end, ok, parseErr := parseCSSURL(css, i)
			if parseErr != nil {
				return nil, parseErr
			}
			if !ok {
				i++
				continue
			}
			font := len(blocks) > 0 && blocks[len(blocks)-1]
			if !pendingImport {
				references = append(references, discoveredFormatCSSURL{value: value, font: font})
			}
			i = end
		default:
			i++
		}
	}
	return references, nil
}

func cssNameAt(source string, offset int, name string) bool {
	if offset < 0 || offset+len(name) > len(source) || !strings.EqualFold(source[offset:offset+len(name)], name) {
		return false
	}
	if offset > 0 && isCSSNameByte(source[offset-1]) {
		return false
	}
	return offset+len(name) == len(source) || !isCSSNameByte(source[offset+len(name)])
}

func isCSSNameByte(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || value == '-' || value == '_'
}

func scanCSSString(source string, offset int) (int, error) {
	quote := source[offset]
	for i := offset + 1; i < len(source); i++ {
		if source[i] == '\\' {
			i++
			continue
		}
		if source[i] == quote {
			return i + 1, nil
		}
		if source[i] == '\n' || source[i] == '\r' {
			return 0, errors.New("newline in CSS string")
		}
	}
	return 0, errors.New("unterminated CSS string")
}

func parseCSSURL(source string, offset int) (string, int, bool, error) {
	i := offset + len("url")
	for i < len(source) && isCSSWhitespace(source[i]) {
		i++
	}
	if i >= len(source) || source[i] != '(' {
		return "", 0, false, nil
	}
	i++
	for i < len(source) && isCSSWhitespace(source[i]) {
		i++
	}
	if i >= len(source) {
		return "", 0, false, errors.New("unterminated CSS url()")
	}
	var raw string
	if source[i] == '\'' || source[i] == '"' {
		start := i + 1
		end, err := scanCSSString(source, i)
		if err != nil {
			return "", 0, false, err
		}
		raw = source[start : end-1]
		i = end
		for i < len(source) && isCSSWhitespace(source[i]) {
			i++
		}
		if i >= len(source) || source[i] != ')' {
			return "", 0, false, errors.New("invalid quoted CSS url()")
		}
	} else {
		start := i
		for i < len(source) && source[i] != ')' {
			if source[i] == '\'' || source[i] == '"' || source[i] == '(' || source[i] == '\n' || source[i] == '\r' {
				return "", 0, false, errors.New("invalid unquoted CSS url()")
			}
			if source[i] == '\\' && i+1 < len(source) {
				i++
			}
			i++
		}
		if i >= len(source) {
			return "", 0, false, errors.New("unterminated CSS url()")
		}
		raw = strings.TrimSpace(source[start:i])
	}
	decoded, err := unescapeCSS(raw)
	if err != nil {
		return "", 0, false, err
	}
	return decoded, i + 1, true, nil
}

func unescapeCSS(value string) (string, error) {
	var result strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] != '\\' {
			result.WriteByte(value[i])
			continue
		}
		if i+1 >= len(value) {
			return "", errors.New("trailing escape in CSS URL")
		}
		i++
		if value[i] == '\n' {
			continue
		}
		if value[i] == '\r' {
			if i+1 < len(value) && value[i+1] == '\n' {
				i++
			}
			continue
		}
		if !isCSSHex(value[i]) {
			result.WriteByte(value[i])
			continue
		}
		start := i
		for i+1 < len(value) && i-start < 5 && isCSSHex(value[i+1]) {
			i++
		}
		codepoint, err := strconv.ParseInt(value[start:i+1], 16, 32)
		if err != nil || codepoint == 0 || codepoint > 0x10ffff {
			return "", errors.New("invalid hexadecimal escape in CSS URL")
		}
		result.WriteRune(rune(codepoint))
		if i+1 < len(value) && isCSSWhitespace(value[i+1]) {
			i++
		}
	}
	return result.String(), nil
}

func isCSSWhitespace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\n' || value == '\r' || value == '\f'
}

func isCSSHex(value byte) bool {
	return value >= '0' && value <= '9' || value >= 'a' && value <= 'f' || value >= 'A' && value <= 'F'
}
