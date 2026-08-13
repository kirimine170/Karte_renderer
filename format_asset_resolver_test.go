package renderer

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestResolveFormatPackageAssets(t *testing.T) {
	root := filepath.Join("testdata", "karte-format", "asset-resolver")
	resolved, err := ResolveFormatPackageAssets(root)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(resolved.Root) {
		t.Fatalf("root = %q, want absolute path", resolved.Root)
	}
	if resolved.MarkdownLayout.PackagePath != "markdown/layout.html" || resolved.AssetsDirectory.PackagePath != "assets" {
		t.Fatalf("unexpected declared paths: %#v", resolved)
	}

	want := []FormatAssetReference{
		{Source: "markdown/base.css", Reference: "../assets/fonts/Report%20Sans.woff2?#v1", PackagePath: "assets/fonts/Report Sans.woff2", Kind: FormatAssetFont, Disposition: FormatAssetLocal},
		{Source: "markdown/base.css", Reference: "../assets/images/paper%20texture.png", PackagePath: "assets/images/paper texture.png", Kind: FormatAssetImage, Disposition: FormatAssetLocal},
		{Source: "markdown/base.css", Reference: "#karte-mark", Kind: FormatAssetImage, Disposition: FormatAssetEmbedded},
		{Source: "markdown/base.css", Reference: "data:image/svg+xml,%3Csvg%3E%3C/svg%3E", Kind: FormatAssetImage, Disposition: FormatAssetEmbedded},
		{Source: "marp/karte.css", Reference: "../assets/fonts/Report Sans.woff2", PackagePath: "assets/fonts/Report Sans.woff2", Kind: FormatAssetFont, Disposition: FormatAssetLocal},
		{Source: "marp/karte.css", Reference: "../assets/images/slide.svg", PackagePath: "assets/images/slide.svg", Kind: FormatAssetImage, Disposition: FormatAssetLocal},
	}
	got := append([]FormatAssetReference(nil), resolved.References...)
	for i := range got {
		got[i].Path = ""
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("references = %#v, want %#v", got, want)
	}
	for _, reference := range resolved.References {
		if reference.Disposition == FormatAssetLocal {
			if !filepath.IsAbs(reference.Path) {
				t.Errorf("local reference path = %q, want absolute path", reference.Path)
			}
			if _, err := os.Stat(reference.Path); err != nil {
				t.Errorf("local reference %q: %v", reference.PackagePath, err)
			}
		} else if reference.Path != "" || reference.PackagePath != "" {
			t.Errorf("embedded reference has filesystem path: %#v", reference)
		}
	}
	wantDistribution := []string{
		"karte-format.yaml",
		"markdown/layout.html",
		"markdown/base.css",
		"marp/karte.css",
		"assets/fonts/Report Sans.woff2",
		"assets/images/paper texture.png",
		"assets/images/slide.svg",
	}
	var gotDistribution []string
	for _, file := range resolved.Distribution {
		gotDistribution = append(gotDistribution, file.PackagePath)
		if file.Kind != FormatPathFile {
			t.Errorf("distribution member %#v is not a file", file)
		}
	}
	if !reflect.DeepEqual(gotDistribution, wantDistribution) {
		t.Fatalf("distribution = %#v, want %#v", gotDistribution, wantDistribution)
	}
}

func TestFormatAssetResolverDiagnostics(t *testing.T) {
	root := t.TempDir()
	writeFormatTestFile(t, filepath.Join(root, "assets", "image.png"), "image")
	writeFormatTestFile(t, filepath.Join(root, "styles", "theme.css"), "")
	resolver, err := NewFormatAssetResolver(root)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		path string
		kind FormatPathKind
		code FormatResolverErrorCode
	}{
		{name: "parent traversal", path: "../outside.png", kind: FormatPathFile, code: FormatResolverOutsidePackage},
		{name: "absolute", path: "/tmp/image.png", kind: FormatPathFile, code: FormatResolverAbsolutePath},
		{name: "Windows drive slash", path: `C:/formats/image.png`, kind: FormatPathFile, code: FormatResolverAbsolutePath},
		{name: "Windows drive backslash", path: `C:\formats\image.png`, kind: FormatPathFile, code: FormatResolverAbsolutePath},
		{name: "Windows UNC", path: `\\server\share\image.png`, kind: FormatPathFile, code: FormatResolverAbsolutePath},
		{name: "URI scheme", path: "https://example.com/image.png", kind: FormatPathFile, code: FormatResolverURIScheme},
		{name: "file URI", path: "file:///tmp/image.png", kind: FormatPathFile, code: FormatResolverURIScheme},
		{name: "missing", path: "assets/missing.png", kind: FormatPathFile, code: FormatResolverPathNotFound},
		{name: "directory used as file", path: "assets", kind: FormatPathFile, code: FormatResolverExpectedFile},
		{name: "file used as directory", path: "assets/image.png", kind: FormatPathDirectory, code: FormatResolverExpectedDirectory},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := resolver.Resolve(test.path, test.kind)
			assertFormatResolverCode(t, err, test.code)
		})
	}
}

func TestFormatAssetResolverCSSReferenceContract(t *testing.T) {
	root := t.TempDir()
	writeFormatTestFile(t, filepath.Join(root, "assets", "images", "paper texture.png"), "image")
	writeFormatTestFile(t, filepath.Join(root, "markdown", "base.css"), "")
	resolver, err := NewFormatAssetResolver(root)
	if err != nil {
		t.Fatal(err)
	}

	valid, err := resolver.ResolveCSSReference("markdown/base.css", "../assets/images/paper%20texture.png?rev=1#preview", false)
	if err != nil {
		t.Fatal(err)
	}
	if valid.PackagePath != "assets/images/paper texture.png" || valid.Disposition != FormatAssetLocal {
		t.Fatalf("valid reference = %#v", valid)
	}
	for _, embedded := range []string{"data:font/woff2;base64,AA==", "#inline-filter"} {
		got, err := resolver.ResolveCSSReference("markdown/base.css", embedded, false)
		if err != nil {
			t.Fatalf("embedded %q: %v", embedded, err)
		}
		if got.Disposition != FormatAssetEmbedded || got.Path != "" {
			t.Fatalf("embedded %q = %#v", embedded, got)
		}
	}

	tests := []struct {
		name string
		ref  string
		code FormatResolverErrorCode
	}{
		{name: "outside traversal", ref: "../../outside.png", code: FormatResolverOutsidePackage},
		{name: "encoded outside traversal", ref: "%2e%2e/%2e%2e/outside.png", code: FormatResolverOutsidePackage},
		{name: "absolute", ref: "/tmp/image.png", code: FormatResolverAbsolutePath},
		{name: "encoded absolute", ref: "%2ftmp/image.png", code: FormatResolverAbsolutePath},
		{name: "protocol relative", ref: "//server/share/image.png", code: FormatResolverAbsolutePath},
		{name: "Windows drive slash", ref: `C:/formats/image.png`, code: FormatResolverAbsolutePath},
		{name: "Windows drive backslash", ref: `C:\formats\image.png`, code: FormatResolverAbsolutePath},
		{name: "Windows UNC", ref: `\\server\share\image.png`, code: FormatResolverAbsolutePath},
		{name: "remote URI", ref: "https://example.com/image.png", code: FormatResolverURIScheme},
		{name: "file URI", ref: "file:///tmp/image.png", code: FormatResolverURIScheme},
		{name: "missing", ref: "../assets/images/missing.png", code: FormatResolverPathNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := resolver.ResolveCSSReference("markdown/base.css", test.ref, false)
			assertFormatResolverCode(t, err, test.code)
		})
	}
}

func TestFormatAssetResolverRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFormatTestFile(t, filepath.Join(root, "styles", "theme.css"), "")
	writeFormatTestFile(t, filepath.Join(outside, "secret.woff2"), "secret")
	if err := os.MkdirAll(filepath.Join(root, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "assets", "escape.woff2")
	if err := os.Symlink(filepath.Join(outside, "secret.woff2"), link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink privilege unavailable: %v", err)
		}
		t.Fatal(err)
	}
	resolver, err := NewFormatAssetResolver(root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = resolver.ResolveCSSReference("styles/theme.css", "../assets/escape.woff2", true)
	assertFormatResolverCode(t, err, FormatResolverSymlinkEscape)
}

func TestFormatAssetResolverAllowsSymlinkWithinPackage(t *testing.T) {
	root := t.TempDir()
	writeFormatTestFile(t, filepath.Join(root, "assets", "fonts", "report.woff2"), "font")
	writeFormatTestFile(t, filepath.Join(root, "styles", "theme.css"), "")
	link := filepath.Join(root, "assets", "font.woff2")
	if err := os.Symlink(filepath.Join(root, "assets", "fonts", "report.woff2"), link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink privilege unavailable: %v", err)
		}
		t.Fatal(err)
	}
	resolver, err := NewFormatAssetResolver(root)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := resolver.ResolveCSSReference("styles/theme.css", "../assets/font.woff2", true)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(resolver.Root(), "assets", "fonts", "report.woff2")
	if resolved.Path != want {
		t.Fatalf("resolved path = %q, want %q", resolved.Path, want)
	}
}

func TestFormatAssetResolverRejectsMissingPathBelowEscapingSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFormatTestFile(t, filepath.Join(root, "styles", "theme.css"), "")
	if err := os.MkdirAll(filepath.Join(root, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "assets", "linked")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink privilege unavailable: %v", err)
		}
		t.Fatal(err)
	}
	resolver, err := NewFormatAssetResolver(root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = resolver.ResolveCSSReference("styles/theme.css", "../assets/linked/missing.png", false)
	assertFormatResolverCode(t, err, FormatResolverSymlinkEscape)
}

func TestResolveFormatPackageAssetsRejectsAssetOutsideDeclaredDirectory(t *testing.T) {
	root := writeFormatResolverPackage(t, `body { background: url("../private/image.png"); }`)
	writeFormatTestFile(t, filepath.Join(root, "private", "image.png"), "image")
	_, err := ResolveFormatPackageAssets(root)
	assertFormatResolverCode(t, err, FormatResolverOutsideAssets)
	if !strings.Contains(err.Error(), "assets.directory") {
		t.Fatalf("error = %v, want assets.directory context", err)
	}
}

func TestResolveFormatPackageAssetsRejectsSymlinkLeavingAssetsDirectory(t *testing.T) {
	root := writeFormatResolverPackage(t, `body { background: url("../assets/private.png"); }`)
	writeFormatTestFile(t, filepath.Join(root, "private", "image.png"), "image")
	link := filepath.Join(root, "assets", "private.png")
	if err := os.Symlink(filepath.Join(root, "private", "image.png"), link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink privilege unavailable: %v", err)
		}
		t.Fatal(err)
	}
	_, err := ResolveFormatPackageAssets(root)
	assertFormatResolverCode(t, err, FormatResolverOutsideAssets)
}

func TestResolveFormatPackageAssetsReportsDeclaredTypeAndMissingPaths(t *testing.T) {
	t.Run("manifest symlink escape", func(t *testing.T) {
		root := writeFormatResolverPackage(t, "")
		outside := filepath.Join(t.TempDir(), FormatManifestFilename)
		writeFormatTestFile(t, outside, validFormatManifest)
		if err := os.Remove(filepath.Join(root, FormatManifestFilename)); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(root, FormatManifestFilename)); err != nil {
			if runtime.GOOS == "windows" {
				t.Skipf("symlink privilege unavailable: %v", err)
			}
			t.Fatal(err)
		}
		_, err := ResolveFormatPackageAssets(root)
		assertFormatResolverCode(t, err, FormatResolverSymlinkEscape)
	})

	t.Run("layout is directory", func(t *testing.T) {
		root := writeFormatResolverPackage(t, "")
		if err := os.Remove(filepath.Join(root, "markdown", "layout.html")); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(root, "markdown", "layout.html"), 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := ResolveFormatPackageAssets(root)
		assertFormatResolverCode(t, err, FormatResolverExpectedFile)
	})

	t.Run("assets is file", func(t *testing.T) {
		root := writeFormatResolverPackage(t, "")
		if err := os.Remove(filepath.Join(root, "assets", "placeholder")); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(root, "assets")); err != nil {
			t.Fatal(err)
		}
		writeFormatTestFile(t, filepath.Join(root, "assets"), "not a directory")
		_, err := ResolveFormatPackageAssets(root)
		assertFormatResolverCode(t, err, FormatResolverExpectedDirectory)
	})

	t.Run("theme is missing", func(t *testing.T) {
		root := writeFormatResolverPackage(t, "")
		if err := os.Remove(filepath.Join(root, "marp", "karte.css")); err != nil {
			t.Fatal(err)
		}
		_, err := ResolveFormatPackageAssets(root)
		assertFormatResolverCode(t, err, FormatResolverPathNotFound)
	})
}

func TestDiscoverFormatCSSURLsIgnoresCommentsAndStrings(t *testing.T) {
	root := t.TempDir()
	css := filepath.Join(root, "theme.css")
	writeFormatTestFile(t, css, `
/* url("ignored-comment.png") */
.example::before { content: 'url("ignored-string.png")'; }
@import url("ignored-import.css");
@font-face { src: local("Report Sans"), url("font.woff2") format("woff2"); }
.page { background: URL( image.png ); }
`)
	got, err := discoverFormatCSSURLs(css)
	if err != nil {
		t.Fatal(err)
	}
	want := []discoveredFormatCSSURL{{value: "font.woff2", font: true}, {value: "image.png", font: false}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("references = %#v, want %#v", got, want)
	}
}

func assertFormatResolverCode(t *testing.T, err error, want FormatResolverErrorCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want code %q", want)
	}
	var resolverErr *FormatResolverError
	if !errors.As(err, &resolverErr) {
		t.Fatalf("error = %T %v, want *FormatResolverError", err, err)
	}
	if resolverErr.Code != want {
		t.Fatalf("error code = %q, want %q; error: %v", resolverErr.Code, want, err)
	}
}

func writeFormatResolverPackage(t *testing.T, stylesheet string) string {
	t.Helper()
	root := t.TempDir()
	writeFormatTestFile(t, filepath.Join(root, FormatManifestFilename), validFormatManifest)
	writeFormatTestFile(t, filepath.Join(root, "markdown", "layout.html"), "{{CONTENT}}")
	writeFormatTestFile(t, filepath.Join(root, "markdown", "base.css"), stylesheet)
	writeFormatTestFile(t, filepath.Join(root, "markdown", "print.css"), "")
	writeFormatTestFile(t, filepath.Join(root, "marp", "karte.css"), "")
	writeFormatTestFile(t, filepath.Join(root, "assets", "placeholder"), "asset")
	return root
}

func writeFormatTestFile(t *testing.T, filename, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
