package renderer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

type reportFixtureExpectation struct {
	ProgressPercent int `json:"progressPercent"`
	Document        struct {
		Title    string   `json:"title"`
		Headings []string `json:"headings"`
		Asset    string   `json:"asset"`
		Pages    int      `json:"pages"`
	} `json:"document"`
	Marp struct {
		Title  string `json:"title"`
		Theme  string `json:"theme"`
		Slides int    `json:"slides"`
		Asset  string `json:"asset"`
	} `json:"marp"`
	Resolution struct {
		Distribution []string `json:"distribution"`
		References   []struct {
			Source      string `json:"source"`
			Reference   string `json:"reference"`
			PackagePath string `json:"packagePath"`
		} `json:"references"`
	} `json:"resolution"`
}

func TestActivityReportFormatFixtureResolvesExpectedPackage(t *testing.T) {
	root := activityReportFixtureRoot()
	expected := loadActivityReportExpectation(t, root)
	resolved, err := ResolveFormatPackageAssets(root)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Manifest.Name != "karte-activity-report" || resolved.Manifest.Version != "0.1.0" {
		t.Fatalf("unexpected package identity: %#v", resolved.Manifest)
	}
	var distribution []string
	for _, member := range resolved.Distribution {
		distribution = append(distribution, member.PackagePath)
	}
	if !reflect.DeepEqual(distribution, expected.Resolution.Distribution) {
		t.Fatalf("distribution = %#v, want %#v", distribution, expected.Resolution.Distribution)
	}
	if len(resolved.References) != len(expected.Resolution.References) {
		t.Fatalf("references = %d, want %d", len(resolved.References), len(expected.Resolution.References))
	}
	for index, want := range expected.Resolution.References {
		got := resolved.References[index]
		if got.Source != want.Source || got.Reference != want.Reference || got.PackagePath != want.PackagePath {
			t.Errorf("reference %d = %#v, want %#v", index, got, want)
		}
		if got.Kind != FormatAssetImage || got.Disposition != FormatAssetLocal {
			t.Errorf("reference %d has unexpected classification: %#v", index, got)
		}
	}
}

func TestActivityReportFormatFixtureSharesThemeTokens(t *testing.T) {
	resolved, err := ResolveFormatPackageAssets(activityReportFixtureRoot())
	if err != nil {
		t.Fatal(err)
	}
	documentTokens := formatThemeTokens(t, resolved.MarkdownStyles[0].Path)
	marpTokens := formatThemeTokens(t, resolved.MarpThemes[0].Path)
	if len(documentTokens) < 10 {
		t.Fatalf("shared token count = %d, want at least 10", len(documentTokens))
	}
	if !reflect.DeepEqual(documentTokens, marpTokens) {
		t.Fatalf("document and Marp tokens differ\ndocument: %#v\nmarp: %#v", documentTokens, marpTokens)
	}
}

func TestActivityReportProgressGraphicMatchesExpectedValue(t *testing.T) {
	root := activityReportFixtureRoot()
	expected := loadActivityReportExpectation(t, root)
	graphic, err := os.ReadFile(filepath.Join(root, expected.Document.Asset))
	if err != nil {
		t.Fatal(err)
	}
	wantY := 320 - (expected.ProgressPercent*272+50)/100
	for _, marker := range []string{
		fmt.Sprintf(`820,%d`, wantY),
		fmt.Sprintf(`>%d%%</text>`, expected.ProgressPercent),
	} {
		if !strings.Contains(string(graphic), marker) {
			t.Errorf("progress graphic is missing %q", marker)
		}
	}
}

func TestActivityReportDocumentRendersExpectedHTML(t *testing.T) {
	root, _, expected := stageActivityReportFixture(t)
	output := filepath.Join(root, "build", "activity-report.html")
	fm, err := ConvertFile(context.Background(), filepath.Join(root, "fixtures", "activity-report.md"), output, ConvertOptions{Root: root, CSS: filepath.Join(root, "report.css")})
	if err != nil {
		t.Fatal(err)
	}
	if fm.Title != expected.Document.Title {
		t.Fatalf("title = %q, want %q", fm.Title, expected.Document.Title)
	}
	html, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	text := string(html)
	for _, marker := range append([]string{`class="report-shell"`, `class="kpi-grid"`, `src="../assets/progress.svg"`, `--karte-accent: #6f42c1`}, expected.Document.Headings...) {
		if !strings.Contains(text, marker) {
			t.Errorf("rendered HTML is missing %q", marker)
		}
	}
	assets := checkHTMLAssets(output)
	if !assets.Passed {
		t.Fatalf("rendered HTML assets failed preflight: %+v", assets)
	}
}

func activityReportFixtureRoot() string { return filepath.Join("examples", "karte-format-report") }

func stageActivityReportFixture(t *testing.T) (string, FormatPackageAssets, reportFixtureExpectation) {
	t.Helper()
	sourceRoot := activityReportFixtureRoot()
	expected := loadActivityReportExpectation(t, sourceRoot)
	source, err := ResolveFormatPackageAssets(sourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "karte-format-report")
	for _, member := range source.Distribution {
		copyReportFixtureFile(t, member.Path, filepath.Join(root, filepath.FromSlash(member.PackagePath)))
	}
	for _, fixture := range []string{"activity-report.md", "activity-report.marp.md"} {
		copyReportFixtureFile(t, filepath.Join(sourceRoot, "fixtures", fixture), filepath.Join(root, "fixtures", fixture))
	}
	copyReportFixtureFile(t, source.MarkdownLayout.Path, filepath.Join(root, "themes", "default", "layout.html"))
	resolved, err := ResolveFormatPackageAssets(root)
	if err != nil {
		t.Fatal(err)
	}
	var css strings.Builder
	for _, stylesheet := range resolved.MarkdownStyles {
		contents, readErr := os.ReadFile(stylesheet.Path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		css.Write(contents)
		css.WriteByte('\n')
	}
	writeFile(t, filepath.Join(root, "report.css"), css.String())
	return root, resolved, expected
}

func loadActivityReportExpectation(t *testing.T, root string) reportFixtureExpectation {
	t.Helper()
	source, err := os.ReadFile(filepath.Join(root, "expected.json"))
	if err != nil {
		t.Fatal(err)
	}
	var expected reportFixtureExpectation
	if err := json.Unmarshal(source, &expected); err != nil {
		t.Fatal(err)
	}
	return expected
}

func formatThemeTokens(t *testing.T, filename string) map[string]string {
	t.Helper()
	source, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	tokens := map[string]string{}
	pattern := regexp.MustCompile(`(?m)^\s*(--karte-[a-z0-9-]+):\s*([^;]+);`)
	for _, match := range pattern.FindAllStringSubmatch(string(source), -1) {
		tokens[match[1]] = strings.TrimSpace(match[2])
	}
	return tokens
}

func copyReportFixtureFile(t *testing.T, source, destination string) {
	t.Helper()
	contents, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, destination, string(contents))
}
