package renderer

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestPreviewResultJSONGolden(t *testing.T) {
	result := PreviewResult{
		SchemaVersion: PreviewSchemaVersion,
		Kind:          PreviewKindMarp,
		PageMode:      PreviewPageSlides,
		HTML:          `<section data-marpit-svg><h1>Preview</h1></section>`,
		Pages: []PreviewPage{{
			ID:       "karte-intro",
			Index:    0,
			Editable: true,
			Source: &PreviewSourceRange{
				Path:       "slides/intro.md",
				StartByte:  20,
				EndByte:    43,
				StartUTF16: 20,
				EndUTF16:   41,
				StartLine:  3,
				EndLine:    5,
			},
		}},
		Metadata: RenderMetadata{
			SchemaVersion: RenderMetadataSchemaVersion,
			FrontMatter: FrontMatter{
				Title: "Preview contract",
				Marp:  true,
			},
		},
		Diagnostics: []RenderDiagnostic{{
			Severity: DiagnosticWarning,
			Code:     "preview_source_unmapped",
			Message:  "an imported page cannot be mapped to editable source",
			Path:     "partials/details.md",
		}},
	}

	got, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "schemaVersion": 1,
  "kind": "marp",
  "pageMode": "slides",
  "html": "\u003csection data-marpit-svg\u003e\u003ch1\u003ePreview\u003c/h1\u003e\u003c/section\u003e",
  "pages": [
    {
      "id": "karte-intro",
      "index": 0,
      "editable": true,
      "source": {
        "path": "slides/intro.md",
        "startByte": 20,
        "endByte": 43,
        "startUtf16": 20,
        "endUtf16": 41,
        "startLine": 3,
        "endLine": 5
      }
    }
  ],
  "metadata": {
    "schemaVersion": 1,
    "frontMatter": {
      "title": "Preview contract",
      "marp": true
    }
  },
  "diagnostics": [
    {
      "severity": "warning",
      "code": "preview_source_unmapped",
      "message": "an imported page cannot be mapped to editable source",
      "path": "partials/details.md"
    }
  ]
}`
	if string(got) != want {
		t.Fatalf("PreviewResult JSON changed\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestPreviewResultJSONOmitsOnlyOptionalCollections(t *testing.T) {
	result := PreviewResult{
		SchemaVersion: PreviewSchemaVersion,
		Kind:          PreviewKindMarkdown,
		PageMode:      PreviewPageContinuous,
		HTML:          "",
		Metadata: RenderMetadata{
			SchemaVersion: RenderMetadataSchemaVersion,
		},
	}

	got, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"schemaVersion":1,"kind":"markdown","pageMode":"continuous","html":"","metadata":{"schemaVersion":1,"frontMatter":{}}}`
	if string(got) != want {
		t.Fatalf("PreviewResult JSON = %s, want %s", got, want)
	}
}

func TestPreviewContractSchemaVersionAndWireValues(t *testing.T) {
	if PreviewSchemaVersion != 1 {
		t.Fatalf("PreviewSchemaVersion = %d, want 1", PreviewSchemaVersion)
	}

	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "markdown kind", got: string(PreviewKindMarkdown), want: "markdown"},
		{name: "marp kind", got: string(PreviewKindMarp), want: "marp"},
		{name: "continuous mode", got: string(PreviewPageContinuous), want: "continuous"},
		{name: "slides mode", got: string(PreviewPageSlides), want: "slides"},
		{name: "runtime mode", got: string(PreviewPageRuntime), want: "runtime-pages"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("wire value = %q, want %q", tt.got, tt.want)
			}
		})
	}
}

func TestPreviewContractDocumentedJSONSchema(t *testing.T) {
	document, err := os.ReadFile("docs/preview-contract.md")
	if err != nil {
		t.Fatal(err)
	}
	documentText := strings.ReplaceAll(string(document), "\r\n", "\n")

	const schemaStart = "{\n  \"$schema\": \"https://json-schema.org/draft/2020-12/schema\""
	start := strings.Index(documentText, schemaStart)
	if start < 0 {
		t.Fatal("documented PreviewResult JSON Schema was not found")
	}
	end := strings.Index(documentText[start:], "\n```")
	if end < 0 {
		t.Fatal("documented PreviewResult JSON Schema is not a closed code block")
	}
	schemaJSON := []byte(documentText[start : start+end])
	if !json.Valid(schemaJSON) {
		t.Fatalf("documented PreviewResult JSON Schema is not valid JSON:\n%s", schemaJSON)
	}

	var schema struct {
		Properties map[string]struct {
			Const int `json:"const"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(schemaJSON, &schema); err != nil {
		t.Fatal(err)
	}
	if got := schema.Properties["schemaVersion"].Const; got != PreviewSchemaVersion {
		t.Fatalf("documented schema version = %d, want %d", got, PreviewSchemaVersion)
	}
}

func TestPreviewPageJSONDistinguishesUnmappedSource(t *testing.T) {
	page := PreviewPage{ID: "marp-unmapped-1", Index: 1, Editable: false}
	got, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"marp-unmapped-1","index":1,"editable":false}`
	if string(got) != want {
		t.Fatalf("PreviewPage JSON = %s, want %s", got, want)
	}
}

func TestRenderResultJSONRemainsBackwardCompatible(t *testing.T) {
	legacy := RenderResult{
		HTML: `<main>legacy</main>`,
		Metadata: RenderMetadata{
			SchemaVersion: RenderMetadataSchemaVersion,
			FrontMatter: FrontMatter{
				Title: "Legacy result",
			},
		},
	}

	got, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"html":"\u003cmain\u003elegacy\u003c/main\u003e","metadata":{"schemaVersion":1,"frontMatter":{"title":"Legacy result"}}}`
	if string(got) != want {
		t.Fatalf("RenderResult JSON changed: got %s, want %s", got, want)
	}
}
