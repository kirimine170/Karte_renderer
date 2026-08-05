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

const validFormatManifest = `schemaVersion: 1
name: karte-default
version: 1.0.0

markdown:
  layout: markdown/layout.html
  styles:
    - markdown/base.css
    - markdown/print.css

marp:
  defaultTheme: karte
  themes:
    - marp/karte.css

assets:
  directory: assets
`

func TestParseFormatManifest(t *testing.T) {
	got, err := ParseFormatManifest([]byte(validFormatManifest))
	if err != nil {
		t.Fatal(err)
	}
	want := FormatManifest{
		SchemaVersion: 1,
		Name:          "karte-default",
		Version:       "1.0.0",
		Markdown: FormatMarkdownManifest{
			Layout: "markdown/layout.html",
			Styles: []string{"markdown/base.css", "markdown/print.css"},
		},
		Marp: FormatMarpManifest{
			DefaultTheme: "karte",
			Themes:       []string{"marp/karte.css"},
		},
		Assets: FormatAssetsManifest{Directory: "assets"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("manifest = %#v, want %#v", got, want)
	}
}

func TestLoadFormatManifest(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, FormatManifestFilename), validFormatManifest)
	got, err := LoadFormatManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "karte-default" || got.Markdown.Layout != "markdown/layout.html" || got.Marp.DefaultTheme != "karte" || got.Assets.Directory != "assets" {
		t.Fatalf("unexpected manifest: %#v", got)
	}
}

func TestMinimalFormatPackageExample(t *testing.T) {
	root := filepath.Join("examples", "karte-format-v0.1")
	manifest, err := LoadFormatManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{
		manifest.Markdown.Layout,
		manifest.Assets.Directory,
	}
	paths = append(paths, manifest.Markdown.Styles...)
	paths = append(paths, manifest.Marp.Themes...)
	for _, relative := range paths {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative))); err != nil {
			t.Fatalf("example path %q: %v", relative, err)
		}
	}

	schema, err := os.ReadFile(filepath.Join("docs", "karte-format-v0.1.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(schema) {
		t.Fatal("karte-format-v0.1.schema.json is not valid JSON")
	}
}

func TestLoadFormatManifestReportsFilename(t *testing.T) {
	_, err := LoadFormatManifest(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), FormatManifestFilename) || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected missing manifest error, got %v", err)
	}
}

func TestParseFormatManifestRejectsInvalidContracts(t *testing.T) {
	tests := []struct {
		name    string
		replace string
		with    string
		want    string
	}{
		{name: "missing schema", replace: "schemaVersion: 1\n", want: "schemaVersion is required"},
		{name: "future schema", replace: "schemaVersion: 1", with: "schemaVersion: 2", want: "unsupported schemaVersion 2"},
		{name: "invalid name", replace: "name: karte-default", with: "name: Karte Default", want: "name must use"},
		{name: "invalid version", replace: "version: 1.0.0", with: "version: latest", want: "semantic version"},
		{name: "missing layout", replace: "  layout: markdown/layout.html\n", want: "markdown.layout is required"},
		{name: "empty styles", replace: "  styles:\n    - markdown/base.css\n    - markdown/print.css", with: "  styles: []", want: "markdown.styles must contain"},
		{name: "missing default theme", replace: "  defaultTheme: karte", with: "  defaultTheme: ''", want: "marp.defaultTheme is required"},
		{name: "empty themes", replace: "  themes:\n    - marp/karte.css", with: "  themes: []", want: "marp.themes must contain"},
		{name: "missing assets", replace: "  directory: assets\n", want: "assets.directory is required"},
		{name: "unknown field", replace: "name: karte-default", with: "name: karte-default\ndisplayName: Karte", want: "field displayName not found"},
		{name: "duplicate styles", replace: "    - markdown/print.css", with: "    - markdown/base.css", want: "duplicate path"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := strings.Replace(validFormatManifest, test.replace, test.with, 1)
			_, err := ParseFormatManifest([]byte(source))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestParseFormatManifestRejectsUnsafePaths(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "parent traversal", path: "../layout.html", want: "escapes the format package"},
		{name: "nested traversal", path: "markdown/../../layout.html", want: "escapes the format package"},
		{name: "absolute", path: "/tmp/layout.html", want: "package-relative local path"},
		{name: "windows drive", path: `C:/formats/layout.html`, want: "package-relative local path"},
		{name: "backslash", path: `markdown\layout.html`, want: "forward slashes"},
		{name: "remote URL", path: "https://example.com/layout.html", want: "package-relative local path"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := strings.Replace(validFormatManifest, "markdown/layout.html", test.path, 1)
			_, err := ParseFormatManifest([]byte(source))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestParseFormatManifestRejectsMultipleDocuments(t *testing.T) {
	_, err := ParseFormatManifest([]byte(validFormatManifest + "---\nname: second\n"))
	if err == nil || !strings.Contains(err.Error(), "multiple YAML documents") {
		t.Fatalf("expected multiple document error, got %v", err)
	}
}
