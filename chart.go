package renderer

import (
	"encoding/csv"
	"fmt"
	"html"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	defaultChartWidth  = 720
	defaultChartHeight = 420
)

type chartSpec struct {
	Type                     string
	Path                     string
	X, Y, Series             string
	ID, Caption, Source      string
	Title, XLabel, YLabel    string
	XUnit, YUnit, Note       string
	Width, Height, Histogram int
}

type chartDatum struct {
	x, y     float64
	category string
	series   string
}

type chartDataset struct {
	records    []chartDatum
	series     []string
	categories []string
}

func (r *Renderer) expandCharts(root, baseDir, source string) (string, error) {
	lines := strings.SplitAfter(source, "\n")
	var out strings.Builder
	codeFence := byte(0)
	codeFenceLength := 0
	for _, original := range lines {
		line, ending := splitPaginationLine(original)
		if codeFence != 0 {
			out.WriteString(original)
			if isPaginationFenceClose(line, codeFence, codeFenceLength) {
				codeFence = 0
				codeFenceLength = 0
			}
			continue
		}
		if marker, length, ok := paginationFenceOpen(line); ok {
			codeFence = marker
			codeFenceLength = length
			out.WriteString(original)
			continue
		}
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "@chart(") || !strings.HasSuffix(trimmed, ")") {
			out.WriteString(original)
			continue
		}
		if len(line)-len(strings.TrimLeft(line, " ")) > 3 {
			out.WriteString(original)
			continue
		}
		attrs := parseAttrs(strings.TrimSuffix(strings.TrimPrefix(trimmed, "@chart("), ")"))
		spec, err := parseChartSpec(attrs)
		if err != nil {
			return "", err
		}
		full := spec.Path
		if !filepath.IsAbs(full) {
			full = filepath.Join(baseDir, full)
		}
		full = filepath.Clean(full)
		if !isWithin(root, full) {
			return "", fmt.Errorf("chart path escapes root: %s", spec.Path)
		}
		if r.usesOSFileSystem() {
			full, err = resolveWithinRoot(root, full)
			if err != nil {
				return "", fmt.Errorf("resolve chart path %s: %w", spec.Path, err)
			}
		}
		data, err := r.fs.ReadFile(full)
		if err != nil {
			return "", fmt.Errorf("read chart CSV %s: %w", spec.Path, err)
		}
		dataset, err := parseChartCSV(data, spec)
		if err != nil {
			return "", fmt.Errorf("chart %s: %w", spec.Path, err)
		}
		svg, err := renderChartSVG(spec, dataset)
		if err != nil {
			return "", fmt.Errorf("chart %s: %w", spec.Path, err)
		}
		out.WriteString(svg)
		out.WriteString(ending)
	}
	return out.String(), nil
}

func parseChartSpec(attrs map[string]string) (chartSpec, error) {
	var err error
	spec := chartSpec{
		Type: strings.ToLower(strings.TrimSpace(attrs["type"])), Path: strings.TrimSpace(attrs["path"]),
		X: attrs["x"], Y: attrs["y"], Series: attrs["series"], Title: attrs["title"],
		ID: attrs["id"], Caption: attrs["caption"], Source: attrs["source"],
		XLabel: attrs["xLabel"], YLabel: attrs["yLabel"], XUnit: attrs["xUnit"],
		YUnit: attrs["yUnit"], Note: attrs["note"], Width: defaultChartWidth,
		Height: defaultChartHeight, Histogram: 10,
	}
	if spec.Type == "" {
		spec.Type = "scatter"
	}
	switch spec.Type {
	case "scatter", "line", "bar", "histogram":
	default:
		return chartSpec{}, fmt.Errorf("unsupported chart type %q (use scatter, line, bar, or histogram)", spec.Type)
	}
	if spec.Path == "" {
		return chartSpec{}, fmt.Errorf("@chart missing path")
	}
	spec.ID, err = optionalFigureID(spec.ID, "@chart")
	if err != nil {
		return chartSpec{}, err
	}
	if spec.X == "" {
		return chartSpec{}, fmt.Errorf("@chart missing x column")
	}
	if spec.Type != "histogram" && spec.Y == "" {
		return chartSpec{}, fmt.Errorf("@chart type %s missing y column", spec.Type)
	}
	if attrs["width"] != "" {
		spec.Width, err = chartInteger(attrs["width"], "width", 320, 2000)
		if err != nil {
			return chartSpec{}, err
		}
	}
	if attrs["height"] != "" {
		spec.Height, err = chartInteger(attrs["height"], "height", 240, 1600)
		if err != nil {
			return chartSpec{}, err
		}
	}
	if attrs["bins"] != "" {
		spec.Histogram, err = chartInteger(attrs["bins"], "bins", 1, 100)
		if err != nil {
			return chartSpec{}, err
		}
	}
	return spec, nil
}

func chartInteger(value, name string, minimum, maximum int) (int, error) {
	n, err := strconv.Atoi(value)
	if err != nil || n < minimum || n > maximum {
		return 0, fmt.Errorf("invalid chart %s %q (expected %d-%d)", name, value, minimum, maximum)
	}
	return n, nil
}

func parseChartCSV(data []byte, spec chartSpec) (chartDataset, error) {
	records, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
	if err != nil {
		return chartDataset{}, fmt.Errorf("parse CSV: %w", err)
	}
	if len(records) < 2 {
		return chartDataset{}, fmt.Errorf("CSV must contain a header and at least one data row")
	}
	columns := make(map[string]int, len(records[0]))
	for i, name := range records[0] {
		columns[strings.TrimSpace(name)] = i
	}
	required := []string{spec.X}
	if spec.Type != "histogram" {
		required = append(required, spec.Y)
	}
	if spec.Series != "" {
		required = append(required, spec.Series)
	}
	for _, name := range required {
		if _, ok := columns[name]; !ok {
			return chartDataset{}, fmt.Errorf("missing CSV column %q", name)
		}
	}

	dataset := chartDataset{}
	seriesSeen := map[string]bool{}
	categorySeen := map[string]bool{}
	for rowIndex, row := range records[1:] {
		cell := func(name string) string {
			index := columns[name]
			if index >= len(row) {
				return ""
			}
			return strings.TrimSpace(row[index])
		}
		series := "Data"
		if spec.Series != "" {
			series = cell(spec.Series)
			if series == "" {
				return chartDataset{}, fmt.Errorf("row %d has an empty series value", rowIndex+2)
			}
		}
		if !seriesSeen[series] {
			seriesSeen[series] = true
			dataset.series = append(dataset.series, series)
		}

		datum := chartDatum{series: series}
		if spec.Type == "bar" {
			datum.category = cell(spec.X)
			if datum.category == "" {
				return chartDataset{}, fmt.Errorf("row %d has an empty category", rowIndex+2)
			}
			if !categorySeen[datum.category] {
				categorySeen[datum.category] = true
				dataset.categories = append(dataset.categories, datum.category)
			}
		} else {
			datum.x, err = chartNumber(cell(spec.X), spec.X, rowIndex+2)
			if err != nil {
				return chartDataset{}, err
			}
		}
		if spec.Type != "histogram" {
			datum.y, err = chartNumber(cell(spec.Y), spec.Y, rowIndex+2)
			if err != nil {
				return chartDataset{}, err
			}
		}
		dataset.records = append(dataset.records, datum)
	}
	return dataset, nil
}

func chartNumber(value, column string, row int) (float64, error) {
	n, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, fmt.Errorf("row %d column %q has invalid number %q", row, column, value)
	}
	return n, nil
}

func renderChartSVG(spec chartSpec, dataset chartDataset) (string, error) {
	if len(dataset.records) == 0 {
		return "", fmt.Errorf("chart dataset is empty")
	}
	left, top, bottom, right := 72.0, 52.0, 66.0, 32.0
	if spec.Note != "" {
		bottom = 82
	}
	showLegend := spec.Series != "" && spec.Type != "histogram"
	if showLegend {
		right = 150
	}
	plotWidth := float64(spec.Width) - left - right
	plotHeight := float64(spec.Height) - top - bottom
	if plotWidth <= 0 || plotHeight <= 0 {
		return "", fmt.Errorf("chart dimensions leave no plotting area")
	}

	var body strings.Builder
	if spec.Title != "" {
		fmt.Fprintf(&body, `<text x="%s" y="26" text-anchor="middle" font-size="18" font-weight="600">%s</text>`, chartFloat(float64(spec.Width)/2), html.EscapeString(spec.Title))
	}

	var xMin, xMax, yMin, yMax float64
	switch spec.Type {
	case "bar":
		xMin, xMax = 0, float64(len(dataset.categories))
		yMin, yMax = chartRange(chartYValues(dataset.records), true)
	case "histogram":
		xMin, xMax = chartRange(chartXValues(dataset.records), false)
		counts := histogramCounts(dataset.records, xMin, xMax, spec.Histogram)
		yMin, yMax = 0, float64(maxInt(counts))
		if yMax == 0 {
			yMax = 1
		}
	default:
		xMin, xMax = chartRange(chartXValues(dataset.records), false)
		yMin, yMax = chartRange(chartYValues(dataset.records), false)
	}

	drawChartAxes(&body, spec, dataset, left, top, plotWidth, plotHeight, xMin, xMax, yMin, yMax)
	switch spec.Type {
	case "scatter":
		drawScatter(&body, dataset, left, top, plotWidth, plotHeight, xMin, xMax, yMin, yMax)
	case "line":
		drawLines(&body, dataset, left, top, plotWidth, plotHeight, xMin, xMax, yMin, yMax)
	case "bar":
		drawBars(&body, dataset, left, top, plotWidth, plotHeight, yMin, yMax)
	case "histogram":
		drawHistogram(&body, dataset.records, spec.Histogram, left, top, plotWidth, plotHeight, xMin, xMax, yMax)
	}
	if showLegend {
		drawLegend(&body, dataset.series, left+plotWidth+20, top+8)
	}
	if spec.Note != "" {
		fmt.Fprintf(&body, `<text x="%s" y="%d" text-anchor="middle" font-size="11">%s</text>`, chartFloat(float64(spec.Width)/2), spec.Height-10, html.EscapeString(spec.Note))
	}

	label := spec.Title
	if label == "" {
		label = spec.Type + " chart"
	}
	chart := fmt.Sprintf(`<div class="karte-chart" data-chart-type="%s"><svg xmlns="http://www.w3.org/2000/svg" role="img" aria-label="%s" viewBox="0 0 %d %d" width="%d" height="%d" style="display:block;max-width:100%%;height:auto"><title>%s</title><rect width="100%%" height="100%%" fill="white"/>%s</svg></div>`,
		html.EscapeString(spec.Type), html.EscapeString(label), spec.Width, spec.Height, spec.Width, spec.Height, html.EscapeString(label), body.String())
	return wrapNumberedFigure("figure", spec.ID, "karte-chart-figure", chart, spec.Caption, spec.Source, ""), nil
}

func drawChartAxes(out *strings.Builder, spec chartSpec, dataset chartDataset, left, top, width, height, xMin, xMax, yMin, yMax float64) {
	bottom := top + height
	right := left + width
	for i := 0; i <= 5; i++ {
		ratio := float64(i) / 5
		y := bottom - ratio*height
		value := yMin + ratio*(yMax-yMin)
		fmt.Fprintf(out, `<line x1="%s" y1="%s" x2="%s" y2="%s" stroke="#ddd" stroke-width="1"/><text x="%s" y="%s" text-anchor="end" font-size="11">%s</text>`, chartFloat(left), chartFloat(y), chartFloat(right), chartFloat(y), chartFloat(left-8), chartFloat(y+4), html.EscapeString(chartTick(value)))
	}
	if spec.Type == "bar" {
		for i, category := range dataset.categories {
			x := left + (float64(i)+0.5)*width/float64(len(dataset.categories))
			fmt.Fprintf(out, `<text x="%s" y="%s" text-anchor="middle" font-size="11">%s</text>`, chartFloat(x), chartFloat(bottom+18), html.EscapeString(category))
		}
	} else {
		for i := 0; i <= 5; i++ {
			ratio := float64(i) / 5
			x := left + ratio*width
			value := xMin + ratio*(xMax-xMin)
			fmt.Fprintf(out, `<line x1="%s" y1="%s" x2="%s" y2="%s" stroke="#ddd" stroke-width="1"/><text x="%s" y="%s" text-anchor="middle" font-size="11">%s</text>`, chartFloat(x), chartFloat(top), chartFloat(x), chartFloat(bottom), chartFloat(x), chartFloat(bottom+18), html.EscapeString(chartTick(value)))
		}
	}
	fmt.Fprintf(out, `<line x1="%s" y1="%s" x2="%s" y2="%s" stroke="#111" stroke-width="1.5"/><line x1="%s" y1="%s" x2="%s" y2="%s" stroke="#111" stroke-width="1.5"/>`, chartFloat(left), chartFloat(top), chartFloat(left), chartFloat(bottom), chartFloat(left), chartFloat(bottom), chartFloat(right), chartFloat(bottom))
	xLabel := chartAxisLabel(spec.XLabel, spec.XUnit, spec.X)
	yLabel := chartAxisLabel(spec.YLabel, spec.YUnit, spec.Y)
	if spec.Type == "histogram" && yLabel == "" {
		yLabel = "Count"
	}
	if xLabel != "" {
		fmt.Fprintf(out, `<text x="%s" y="%s" text-anchor="middle" font-size="12">%s</text>`, chartFloat(left+width/2), chartFloat(bottom+44), html.EscapeString(xLabel))
	}
	if yLabel != "" {
		fmt.Fprintf(out, `<text x="18" y="%s" text-anchor="middle" font-size="12" transform="rotate(-90 18 %s)">%s</text>`, chartFloat(top+height/2), chartFloat(top+height/2), html.EscapeString(yLabel))
	}
}

func chartAxisLabel(label, unit, fallback string) string {
	if label == "" {
		label = fallback
	}
	if unit != "" {
		label += " (" + unit + ")"
	}
	return label
}

func drawScatter(out *strings.Builder, dataset chartDataset, left, top, width, height, xMin, xMax, yMin, yMax float64) {
	for _, datum := range dataset.records {
		x := chartScale(datum.x, xMin, xMax, left, left+width)
		y := chartScale(datum.y, yMin, yMax, top+height, top)
		drawMarker(out, x, y, indexOf(dataset.series, datum.series))
	}
}

func drawLines(out *strings.Builder, dataset chartDataset, left, top, width, height, xMin, xMax, yMin, yMax float64) {
	for seriesIndex, series := range dataset.series {
		points := make([]chartDatum, 0)
		for _, datum := range dataset.records {
			if datum.series == series {
				points = append(points, datum)
			}
		}
		sort.SliceStable(points, func(i, j int) bool { return points[i].x < points[j].x })
		var coordinates strings.Builder
		for _, datum := range points {
			x := chartScale(datum.x, xMin, xMax, left, left+width)
			y := chartScale(datum.y, yMin, yMax, top+height, top)
			fmt.Fprintf(&coordinates, "%s,%s ", chartFloat(x), chartFloat(y))
		}
		fmt.Fprintf(out, `<polyline points="%s" fill="none" stroke="#111" stroke-width="2"%s/>`, strings.TrimSpace(coordinates.String()), chartDashAttribute(seriesIndex))
		for _, datum := range points {
			drawMarker(out, chartScale(datum.x, xMin, xMax, left, left+width), chartScale(datum.y, yMin, yMax, top+height, top), seriesIndex)
		}
	}
}

func drawBars(out *strings.Builder, dataset chartDataset, left, top, width, height, yMin, yMax float64) {
	categoryWidth := width / float64(len(dataset.categories))
	barWidth := categoryWidth * 0.72 / float64(len(dataset.series))
	zero := chartScale(0, yMin, yMax, top+height, top)
	for categoryIndex, category := range dataset.categories {
		for seriesIndex, series := range dataset.series {
			for _, datum := range dataset.records {
				if datum.category != category || datum.series != series {
					continue
				}
				x := left + float64(categoryIndex)*categoryWidth + categoryWidth*0.14 + float64(seriesIndex)*barWidth
				y := chartScale(datum.y, yMin, yMax, top+height, top)
				rectY, rectHeight := math.Min(y, zero), math.Abs(zero-y)
				fmt.Fprintf(out, `<rect x="%s" y="%s" width="%s" height="%s" fill="%s" stroke="#111" stroke-width="1"/>`, chartFloat(x), chartFloat(rectY), chartFloat(barWidth), chartFloat(rectHeight), chartFill(seriesIndex))
			}
		}
	}
}

func drawHistogram(out *strings.Builder, records []chartDatum, bins int, left, top, width, height, xMin, xMax, yMax float64) {
	counts := histogramCounts(records, xMin, xMax, bins)
	barWidth := width / float64(bins)
	for i, count := range counts {
		barHeight := float64(count) / yMax * height
		fmt.Fprintf(out, `<rect x="%s" y="%s" width="%s" height="%s" fill="#bbb" stroke="#111" stroke-width="1"/>`, chartFloat(left+float64(i)*barWidth), chartFloat(top+height-barHeight), chartFloat(barWidth), chartFloat(barHeight))
	}
}

func histogramCounts(records []chartDatum, minimum, maximum float64, bins int) []int {
	counts := make([]int, bins)
	for _, datum := range records {
		index := int((datum.x - minimum) / (maximum - minimum) * float64(bins))
		if index >= bins {
			index = bins - 1
		}
		if index < 0 {
			index = 0
		}
		counts[index]++
	}
	return counts
}

func drawLegend(out *strings.Builder, series []string, x, y float64) {
	for i, name := range series {
		rowY := y + float64(i*22)
		fmt.Fprintf(out, `<line x1="%s" y1="%s" x2="%s" y2="%s" stroke="#111" stroke-width="2"%s/>`, chartFloat(x), chartFloat(rowY), chartFloat(x+24), chartFloat(rowY), chartDashAttribute(i))
		drawMarker(out, x+12, rowY, i)
		fmt.Fprintf(out, `<text x="%s" y="%s" font-size="11">%s</text>`, chartFloat(x+32), chartFloat(rowY+4), html.EscapeString(name))
	}
}

func drawMarker(out *strings.Builder, x, y float64, series int) {
	switch series % 4 {
	case 1:
		fmt.Fprintf(out, `<rect x="%s" y="%s" width="7" height="7" fill="white" stroke="#111" stroke-width="1.5"/>`, chartFloat(x-3.5), chartFloat(y-3.5))
	case 2:
		fmt.Fprintf(out, `<path d="M %s %s L %s %s L %s %s Z" fill="white" stroke="#111" stroke-width="1.5"/>`, chartFloat(x), chartFloat(y-4), chartFloat(x+4), chartFloat(y+3.5), chartFloat(x-4), chartFloat(y+3.5))
	case 3:
		fmt.Fprintf(out, `<path d="M %s %s L %s %s L %s %s L %s %s Z" fill="white" stroke="#111" stroke-width="1.5"/>`, chartFloat(x), chartFloat(y-4), chartFloat(x+4), chartFloat(y), chartFloat(x), chartFloat(y+4), chartFloat(x-4), chartFloat(y))
	default:
		fmt.Fprintf(out, `<circle cx="%s" cy="%s" r="3.5" fill="white" stroke="#111" stroke-width="1.5"/>`, chartFloat(x), chartFloat(y))
	}
}

func chartDashAttribute(series int) string {
	dashes := []string{"", "8 4", "2 3", "10 3 2 3"}
	dash := dashes[series%len(dashes)]
	if dash == "" {
		return ""
	}
	return ` stroke-dasharray="` + dash + `"`
}

func chartFill(series int) string {
	return []string{"#222", "#666", "#aaa", "#ddd"}[series%4]
}

func chartRange(values []float64, includeZero bool) (float64, float64) {
	minimum, maximum := values[0], values[0]
	for _, value := range values[1:] {
		minimum = math.Min(minimum, value)
		maximum = math.Max(maximum, value)
	}
	if includeZero {
		minimum = math.Min(minimum, 0)
		maximum = math.Max(maximum, 0)
	}
	if minimum == maximum {
		padding := math.Abs(minimum) * 0.1
		if padding == 0 {
			padding = 1
		}
		minimum -= padding
		maximum += padding
	}
	return minimum, maximum
}

func chartScale(value, domainMin, domainMax, rangeMin, rangeMax float64) float64 {
	return rangeMin + (value-domainMin)/(domainMax-domainMin)*(rangeMax-rangeMin)
}

func chartXValues(records []chartDatum) []float64 {
	values := make([]float64, len(records))
	for i, record := range records {
		values[i] = record.x
	}
	return values
}

func chartYValues(records []chartDatum) []float64 {
	values := make([]float64, len(records))
	for i, record := range records {
		values[i] = record.y
	}
	return values
}

func chartFloat(value float64) string {
	if math.Abs(value) < 0.0000001 {
		value = 0
	}
	formatted := strconv.FormatFloat(value, 'f', 3, 64)
	formatted = strings.TrimRight(strings.TrimRight(formatted, "0"), ".")
	if formatted == "-0" || formatted == "" {
		return "0"
	}
	return formatted
}

func chartTick(value float64) string {
	if math.Abs(value) < 0.0000001 {
		value = 0
	}
	return strconv.FormatFloat(value, 'g', 6, 64)
}

func indexOf(values []string, target string) int {
	for i, value := range values {
		if value == target {
			return i
		}
	}
	return 0
}

func maxInt(values []int) int {
	maximum := 0
	for _, value := range values {
		if value > maximum {
			maximum = value
		}
	}
	return maximum
}
