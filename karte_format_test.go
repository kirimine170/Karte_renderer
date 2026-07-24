package renderer

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type fixtureManifest struct {
	Karte     float64 `yaml:"karte"`
	ID        string  `yaml:"id"`
	Kind      string  `yaml:"kind"`
	Entry     string  `yaml:"entry"`
	Resources map[string]struct {
		Type string `yaml:"type"`
		Path string `yaml:"path"`
	} `yaml:"resources"`
}

func TestComplexKarteFormatManifestResources(t *testing.T) {
	root := complexFixtureRoot()
	b, err := os.ReadFile(filepath.Join(root, "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest fixtureManifest
	if err := yaml.Unmarshal(b, &manifest); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if manifest.Karte != 0.1 || manifest.ID != "complex-report" || manifest.Kind != "document" {
		t.Fatalf("unexpected manifest identity: %+v", manifest)
	}
	entry, err := safeJoin(root, manifest.Entry)
	if err != nil {
		t.Fatalf("invalid entry: %v", err)
	}
	if _, err := os.Stat(entry); err != nil {
		t.Fatalf("entry: %v", err)
	}
	if len(manifest.Resources) != 5 {
		t.Fatalf("resource count = %d, want 5", len(manifest.Resources))
	}
	for id, resource := range manifest.Resources {
		full, err := safeJoin(root, resource.Path)
		if err != nil {
			t.Fatalf("resource %s: %v", id, err)
		}
		if _, err := os.Stat(full); err != nil {
			t.Fatalf("resource %s: %v", id, err)
		}
	}

	image, err := os.ReadFile(filepath.Join(root, manifest.Resources["diagram"].Path))
	if err != nil {
		t.Fatal(err)
	}
	if len(image) < 12 || !bytes.Equal(image[:4], []byte("RIFF")) || !bytes.Equal(image[8:12], []byte("WEBP")) {
		t.Fatal("diagram is not a WebP image")
	}
}

func TestComplexKarteFormatRendersGoldenBody(t *testing.T) {
	root := complexFixtureRoot()
	rendered, fm, err := RenderMarkdown(root, "content/document.md")
	if err != nil {
		t.Fatal(err)
	}
	if fm.Title != "Complex Karte Format Fixture" || len(fm.Owners) != 2 {
		t.Fatalf("unexpected front matter: %+v", fm)
	}
	if strings.Contains(rendered, "@import(") {
		t.Fatal("rendered output contains an unresolved import")
	}

	got := documentBody(t, rendered)
	want, err := os.ReadFile(filepath.Join(root, "expected", "body.html"))
	if err != nil {
		t.Fatal(err)
	}
	if got != strings.TrimSpace(string(want)) {
		t.Fatalf("rendered body changed\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestTeXImportRejectsUnknownDisplayMode(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "formula.tex"), "x < y")
	_, _, err := RenderString(root, `@import(type="tex" path="formula.tex" display="wide")`)
	if err == nil || !strings.Contains(err.Error(), "invalid TeX display mode") {
		t.Fatalf("expected TeX display error, got %v", err)
	}
}

func complexFixtureRoot() string {
	return filepath.Join("testdata", "karte-format", "complex")
}

func documentBody(t *testing.T, document string) string {
	t.Helper()
	start := strings.Index(document, "<body>")
	end := strings.LastIndex(document, "</body>")
	if start < 0 || end < 0 || end < start {
		t.Fatalf("document has no complete body: %s", document)
	}
	return strings.TrimSpace(document[start+len("<body>") : end])
}
