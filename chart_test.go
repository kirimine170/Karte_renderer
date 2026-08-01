package renderer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const chartCSVFixture = `attendance,profit,venue,month
100,20,Hall A,Jan
140,34,Hall A,Feb
180,45,Hall A,Mar
90,14,Hall B,Jan
130,26,Hall B,Feb
170,39,Hall B,Mar
`

func TestChartDirectiveRendersAllSupportedTypes(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "data.csv"), chartCSVFixture)
	for _, chartType := range []string{"scatter", "line", "bar", "histogram"} {
		t.Run(chartType, func(t *testing.T) {
			y := ` y="profit"`
			if chartType == "histogram" {
				y = ""
			}
			source := `@chart(type="` + chartType + `" path="data.csv" x="attendance"` + y + ` series="venue" title="Performance" xLabel="Attendance" xUnit="people" yLabel="Profit" yUnit="JPY" note="Source: fixture")`
			if chartType == "histogram" {
				source = `@chart(type="histogram" path="data.csv" x="profit" bins="4" title="Profit distribution")`
			}
			rendered, _, err := RenderString(root, source)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{`data-chart-type="` + chartType + `"`, `<svg xmlns="http://www.w3.org/2000/svg"`, `<title>`, `stroke="#111"`} {
				assertContains(t, rendered, want)
			}
		})
	}
}

func TestChartRenderingIsDeterministic(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "data.csv"), chartCSVFixture)
	source := `@chart(type="line" path="data.csv" x="attendance" y="profit" series="venue" title="Deterministic")`
	first, _, err := RenderString(root, source)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := RenderString(root, source)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("identical chart input produced different HTML/SVG")
	}
	assertContains(t, first, `stroke-dasharray="8 4"`)
}

func TestChartTicksUseConciseDecimalLabels(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "data.csv"), chartCSVFixture)
	rendered, _, err := RenderString(root, `@chart(type="histogram" path="data.csv" x="profit" bins="5")`)
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, rendered, `>26.4</text>`)
	if strings.Contains(rendered, "26.400000000000") || strings.Contains(rendered, "38.800000000000") {
		t.Fatalf("chart ticks expose floating-point artifacts:\n%s", rendered)
	}
}

func TestChartDirectiveInsideCodeFenceRemainsLiteral(t *testing.T) {
	rendered, _, err := RenderString(t.TempDir(), "```md\n@chart(type=\"scatter\" path=\"missing.csv\" x=\"x\" y=\"y\")\n```")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, rendered, "@chart(type=&quot;scatter&quot;")
	if strings.Contains(rendered, `class="karte-chart"`) {
		t.Fatalf("code-fenced chart directive was executed:\n%s", rendered)
	}
}

func TestChartDirectiveInsideTabIndentedCodeRemainsLiteral(t *testing.T) {
	rendered, _, err := RenderString(t.TempDir(), "\t@chart(type=\"scatter\" path=\"missing.csv\" x=\"x\" y=\"y\")")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, rendered, "<pre><code>@chart")
	if strings.Contains(rendered, `class="karte-chart"`) {
		t.Fatalf("tab-indented chart directive was executed:\n%s", rendered)
	}
}

func TestGeneratedChartTerminatesRawHTMLBlock(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "data.csv"), chartCSVFixture)
	rendered, _, err := RenderString(root, "@chart(type=\"scatter\" path=\"data.csv\" x=\"attendance\" y=\"profit\")\n# After")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, rendered, "<h1>After</h1>")
}

func TestPrepareMarpInputExpandsChartWithoutImports(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "slides.md")
	writeFile(t, filepath.Join(root, "data.csv"), chartCSVFixture)
	source := "---\nmarp: true\n---\n@chart(type=\"scatter\" path=\"data.csv\" x=\"attendance\" y=\"profit\")\n"
	writeFile(t, input, source)
	prepared, cleanup, generatedHTML, err := prepareMarpInput(root, input, source, false)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if prepared == input {
		t.Fatal("chart-only Marp input was not prepared")
	}
	if !generatedHTML {
		t.Fatal("generated chart HTML was not reported to the Marp exporter")
	}
	content, err := os.ReadFile(prepared)
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, string(content), `class="karte-chart"`)
}

func TestBarChartRejectsDuplicateCategoryAndSeries(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "data.csv"), "category,value,series\nA,10,Actual\nA,12,Actual\n")
	_, _, err := RenderString(root, `@chart(type="bar" path="data.csv" x="category" y="value" series="series")`)
	if err == nil || !strings.Contains(err.Error(), `duplicates bar category "A" and series "Actual"`) {
		t.Fatalf("expected duplicate bar key error, got %v", err)
	}
}

func TestChartDirectiveRejectsInvalidInput(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "data.csv"), chartCSVFixture)
	for _, test := range []struct {
		name, directive, want string
	}{
		{"type", `@chart(type="pie" path="data.csv" x="attendance" y="profit")`, "unsupported chart type"},
		{"column", `@chart(type="scatter" path="data.csv" x="missing" y="profit")`, `missing CSV column "missing"`},
		{"dimension", `@chart(type="scatter" path="data.csv" x="attendance" y="profit" width="12")`, "invalid chart width"},
		{"histogram series", `@chart(type="histogram" path="data.csv" x="attendance" series="venue")`, "does not support a series"},
		{"traversal", `@chart(type="scatter" path="../data.csv" x="attendance" y="profit")`, "chart path escapes root"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := RenderString(root, test.directive)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}
		})
	}
}

func TestChartDirectiveRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.csv")
	writeFile(t, outside, chartCSVFixture)
	if err := os.Symlink(outside, filepath.Join(root, "linked.csv")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, _, err := RenderString(root, `@chart(type="scatter" path="linked.csv" x="attendance" y="profit")`)
	if err == nil || !strings.Contains(err.Error(), "escapes root through symlink") {
		t.Fatalf("expected symlink escape error, got %v", err)
	}
}

func TestChartEscapesLabelsAndLegend(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "data.csv"), "x,y,series\n1,2,<unsafe>\n")
	rendered, _, err := RenderString(root, `@chart(type="scatter" path="data.csv" x="x" y="y" series="series" title="A & B")`)
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, rendered, "A &amp; B")
	assertContains(t, rendered, "&lt;unsafe&gt;")
	if strings.Contains(rendered, "<unsafe>") {
		t.Fatalf("chart label was not escaped:\n%s", rendered)
	}
}
