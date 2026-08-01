package renderer

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"math"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// PreflightOptions configures post-render validation for print PDFs.
type PreflightOptions struct {
	Enabled             bool
	ExpectedPages       int
	ExpectedPageSize    string
	ExpectedOrientation string
	ReportPath          string
	ChromiumBinary      string
	PDFInfoBinary       string
	PDFFontsBinary      string
	PDFToTextBinary     string
	SkipOverflowCheck   bool
	SkipFontCheck       bool
	SkipRawTeXCheck     bool
}

// PreflightCheck is one machine-readable validation result.
type PreflightCheck struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail"`
}

// PreflightReport is written after every requested preflight, including
// failures, so CI and release tooling can diagnose the artifact.
type PreflightReport struct {
	SchemaVersion int              `json:"schemaVersion"`
	PDF           string           `json:"pdf"`
	Passed        bool             `json:"passed"`
	Checks        []PreflightCheck `json:"checks"`
}

// PreflightError reports failed checks while retaining the full report.
type PreflightError struct{ Report PreflightReport }

func (e *PreflightError) Error() string {
	failed := make([]string, 0)
	for _, check := range e.Report.Checks {
		if !check.Passed {
			failed = append(failed, check.Name+": "+check.Detail)
		}
	}
	return "PDF preflight failed: " + strings.Join(failed, "; ")
}

func preflightRequested(options PreflightOptions) bool {
	return options.Enabled || options.ExpectedPages != 0
}

func runDocumentPreflight(ctx context.Context, htmlFile, pdfFile string, options PreflightOptions) (PreflightReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	report := PreflightReport{SchemaVersion: 1, PDF: pdfFile, Passed: true}
	if options.ExpectedPages < 0 {
		return report, fmt.Errorf("invalid expected pages %d (must be zero or greater)", options.ExpectedPages)
	}
	if options.ReportPath != "" {
		reportPath, err := validatePreflightReportPath(options.ReportPath, htmlFile, pdfFile)
		if err != nil {
			return report, err
		}
		options.ReportPath = reportPath
	}
	report.Checks = append(report.Checks, checkHTMLAssets(htmlFile))
	if !options.SkipRawTeXCheck {
		report.Checks = append(report.Checks, checkHTMLRawTeX(htmlFile))
	}
	if !options.SkipOverflowCheck {
		report.Checks = append(report.Checks, checkHTMLOverflow(ctx, htmlFile, options))
	}
	report.Checks = append(report.Checks, checkPDFInfo(ctx, pdfFile, options)...)
	if !options.SkipFontCheck {
		report.Checks = append(report.Checks, checkPDFFonts(ctx, pdfFile, options))
	}
	if !options.SkipRawTeXCheck {
		report.Checks = append(report.Checks, checkPDFRawTeX(ctx, pdfFile, options))
	}
	for _, check := range report.Checks {
		if !check.Passed {
			report.Passed = false
			break
		}
	}
	if options.ReportPath != "" {
		if err := writePreflightReport(options.ReportPath, report); err != nil {
			return report, err
		}
	}
	if !report.Passed {
		return report, &PreflightError{Report: report}
	}
	return report, nil
}

func writePreflightReport(path string, report PreflightReport) error {
	content, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode preflight report: %w", err)
	}
	content = append(content, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create preflight report directory: %w", err)
	}
	if err := writeFileAtomic(path, content); err != nil {
		return fmt.Errorf("write preflight report: %w", err)
	}
	return nil
}

func validatePreflightReportPath(path string, protectedPaths ...string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve preflight report path: %w", err)
	}
	abs = filepath.Clean(abs)
	for _, protected := range protectedPaths {
		protectedAbs, err := filepath.Abs(protected)
		if err != nil {
			return "", fmt.Errorf("resolve protected preflight path: %w", err)
		}
		protectedAbs = filepath.Clean(protectedAbs)
		same := abs == protectedAbs
		if runtime.GOOS == "windows" {
			same = strings.EqualFold(abs, protectedAbs)
		}
		if !same {
			reportInfo, reportErr := os.Stat(abs)
			protectedInfo, protectedErr := os.Stat(protectedAbs)
			same = reportErr == nil && protectedErr == nil && os.SameFile(reportInfo, protectedInfo)
		}
		if same {
			return "", fmt.Errorf("preflight report path must not overwrite %s", protectedAbs)
		}
	}
	return abs, nil
}

func checkHTMLAssets(htmlFile string) PreflightCheck {
	document, err := os.ReadFile(htmlFile)
	if err != nil {
		return failedCheck("html-assets", err.Error())
	}
	text := string(document)
	base := (&url.URL{Scheme: "file", Path: filepath.Dir(htmlFile) + string(os.PathSeparator)}).String()
	for _, pattern := range []string{`(?is)<base\b[^>]*\bhref\s*=\s*"([^"]+)"`, `(?is)<base\b[^>]*\bhref\s*=\s*'([^']+)'`} {
		if match := regexp.MustCompile(pattern).FindStringSubmatch(text); match != nil {
			base = html.UnescapeString(match[1])
			break
		}
	}
	references := make([]string, 0)
	for _, pattern := range []string{`(?is)\bsrc\s*=\s*"([^"]+)"`, `(?is)\bsrc\s*=\s*'([^']+)'`, `(?is)<link\b[^>]*\bhref\s*=\s*"([^"]+)"`, `(?is)<link\b[^>]*\bhref\s*=\s*'([^']+)'`} {
		for _, match := range regexp.MustCompile(pattern).FindAllStringSubmatch(text, -1) {
			references = append(references, html.UnescapeString(match[1]))
		}
	}
	for _, style := range regexp.MustCompile(`(?is)<style\b[^>]*>(.*?)</style>`).FindAllStringSubmatch(text, -1) {
		for _, match := range regexp.MustCompile(`url\(\s*["']?([^"')]+)`).FindAllStringSubmatch(style[1], -1) {
			references = append(references, html.UnescapeString(strings.TrimSpace(match[1])))
		}
	}
	missing := make([]string, 0)
	seen := map[string]bool{}
	for _, reference := range references {
		path, local := localAssetPath(base, reference)
		if !local || seen[path] {
			continue
		}
		seen[path] = true
		if _, err := os.Stat(path); err != nil {
			missing = append(missing, reference)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		return failedCheck("html-assets", "missing local assets: "+strings.Join(missing, ", "))
	}
	return passedCheck("html-assets", fmt.Sprintf("validated %d unique local assets", len(seen)))
}

func localAssetPath(base, reference string) (string, bool) {
	reference = strings.TrimSpace(reference)
	if reference == "" || strings.HasPrefix(reference, "#") || strings.HasPrefix(reference, "data:") {
		return "", false
	}
	parsed, err := url.Parse(reference)
	if err != nil || (parsed.Scheme != "" && parsed.Scheme != "file") {
		return "", false
	}
	if parsed.Scheme == "file" {
		return filePathFromURL(parsed, runtime.GOOS), true
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return "", false
	}
	resolved := baseURL.ResolveReference(parsed)
	if resolved.Scheme != "file" {
		return "", false
	}
	return filePathFromURL(resolved, runtime.GOOS), true
}

func filePathFromURL(parsed *url.URL, goos string) string {
	path := parsed.Path
	if goos == "windows" {
		path = strings.ReplaceAll(path, "/", `\`)
		if parsed.Host != "" {
			return `\\` + parsed.Host + path
		}
		if len(path) >= 3 && path[0] == '\\' && path[2] == ':' {
			return path[1:]
		}
		return path
	}
	if parsed.Host != "" {
		path = "//" + parsed.Host + path
	}
	return filepath.FromSlash(path)
}

func checkHTMLRawTeX(htmlFile string) PreflightCheck {
	document, err := os.ReadFile(htmlFile)
	if err != nil {
		return failedCheck("html-raw-tex", err.Error())
	}
	visible := string(document)
	for _, pattern := range []string{
		`(?is)<script\b[^>]*>.*?</script>`, `(?is)<style\b[^>]*>.*?</style>`,
		`(?is)<pre\b[^>]*>.*?</pre>`, `(?is)<code\b[^>]*>.*?</code>`,
		`(?is)<div\b[^>]*data-katex="[^"]*"[^>]*>.*?</div>`,
		`(?is)<span\b[^>]*data-katex="[^"]*"[^>]*>.*?</span>`,
	} {
		visible = regexp.MustCompile(pattern).ReplaceAllString(visible, "")
	}
	visible = regexp.MustCompile(`(?s)<[^>]+>`).ReplaceAllString(visible, " ")
	visible = html.UnescapeString(visible)
	markers := rawTeXMarkers(visible)
	if len(markers) > 0 {
		return failedCheck("html-raw-tex", "visible raw TeX markers: "+strings.Join(markers, ", "))
	}
	return passedCheck("html-raw-tex", "no visible raw TeX markers")
}

func rawTeXMarkers(text string) []string {
	markers := make([]string, 0)
	if strings.Contains(text, "$$$") {
		markers = append(markers, "$$$")
	} else if strings.Contains(text, "$$") {
		markers = append(markers, "$$")
	}
	for _, marker := range []string{`\begin{`, `\end{`, `\[`, `\]`, `\(`, `\)`} {
		if strings.Contains(text, marker) {
			markers = append(markers, marker)
		}
	}
	return markers
}

func checkHTMLOverflow(ctx context.Context, htmlFile string, options PreflightOptions) PreflightCheck {
	binary := options.ChromiumBinary
	if binary == "" {
		binary = findChromium()
	}
	if binary == "" {
		return failedCheck("html-overflow", "Chromium not found for overflow/clip inspection")
	}
	document, err := os.ReadFile(htmlFile)
	if err != nil {
		return failedCheck("html-overflow", err.Error())
	}
	instrumented := injectPreflightProbe(string(document))
	tmp, err := os.CreateTemp(filepath.Dir(htmlFile), ".karte-preflight-*.html")
	if err != nil {
		return failedCheck("html-overflow", err.Error())
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.WriteString(instrumented); err != nil {
		tmp.Close()
		return failedCheck("html-overflow", err.Error())
	}
	if err := tmp.Close(); err != nil {
		return failedCheck("html-overflow", err.Error())
	}
	profile, err := os.MkdirTemp("", "karte-preflight-chromium-*")
	if err != nil {
		return failedCheck("html-overflow", err.Error())
	}
	defer os.RemoveAll(profile)
	width, height := preflightViewport(options.ExpectedPageSize, options.ExpectedOrientation)
	args := []string{
		"--headless", "--disable-gpu", "--disable-extensions", "--allow-file-access-from-files",
		"--run-all-compositor-stages-before-draw", "--virtual-time-budget=1500", "--dump-dom",
		"--user-data-dir=" + profile, fmt.Sprintf("--window-size=%d,%d", width, height),
		(&url.URL{Scheme: "file", Path: tmpName}).String(),
	}
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, binary, args...)
	isolateProcessGroup(cmd)
	cmd.WaitDelay = 2 * time.Second
	output, err := cmd.CombinedOutput()
	if probeCtx.Err() != nil {
		killProcessTree(cmd)
	}
	issues, probeErr := parseOverflowProbe(string(output))
	if err != nil && probeErr != nil {
		return failedCheck("html-overflow", fmt.Sprintf("Chromium layout inspection failed: %v: %s", err, strings.TrimSpace(string(output))))
	}
	if probeErr != nil {
		return failedCheck("html-overflow", probeErr.Error())
	}
	if len(issues) > 0 {
		return failedCheck("html-overflow", "overflow/clipped elements: "+strings.Join(issues, ", "))
	}
	return passedCheck("html-overflow", fmt.Sprintf("no overflow/clip at %dx%d viewport", width, height))
}

func injectPreflightProbe(document string) string {
	probe := `<meta name="karte-preflight-overflow" content="pending"><script id="karte-preflight-probe">(function(){function describe(e){var s=e.tagName.toLowerCase();if(e.id)s+="#"+e.id;if(e.classList&&e.classList.length)s+="."+Array.from(e.classList).slice(0,2).join(".");return s}function check(){var bad=[];var viewport=document.documentElement.clientWidth;document.querySelectorAll("img,svg,table,pre,.katex-display,.karte-figure,.karte-chart-figure,.karte-table-figure").forEach(function(e){var r=e.getBoundingClientRect();var c=getComputedStyle(e);var clipped=((c.overflowX==="hidden"||c.overflowX==="clip")&&e.scrollWidth>e.clientWidth+1)||((c.overflowY==="hidden"||c.overflowY==="clip")&&e.scrollHeight>e.clientHeight+1);var horizontal=r.left<-1||r.right>viewport+1||e.scrollWidth>Math.max(e.clientWidth,viewport)+1;if(clipped||horizontal)bad.push(describe(e))});document.querySelector('meta[name="karte-preflight-overflow"]').setAttribute("content",encodeURIComponent(JSON.stringify(bad)))}if(document.readyState==="complete")setTimeout(check,100);else window.addEventListener("load",function(){setTimeout(check,100)},{once:true})})();</script>`
	lower := strings.ToLower(document)
	if at := strings.Index(lower, "</head>"); at >= 0 {
		return document[:at] + probe + document[at:]
	}
	return probe + document
}

func parseOverflowProbe(document string) ([]string, error) {
	match := regexp.MustCompile(`(?is)<meta\b[^>]*name="karte-preflight-overflow"[^>]*content="([^"]*)"`).FindStringSubmatch(document)
	if match == nil {
		return nil, fmt.Errorf("Chromium overflow probe did not return metadata")
	}
	value := html.UnescapeString(match[1])
	if value == "pending" {
		return nil, fmt.Errorf("Chromium overflow probe did not finish")
	}
	decoded, err := url.QueryUnescape(value)
	if err != nil {
		return nil, fmt.Errorf("decode overflow probe: %w", err)
	}
	var issues []string
	if err := json.Unmarshal([]byte(decoded), &issues); err != nil {
		return nil, fmt.Errorf("parse overflow probe: %w", err)
	}
	return issues, nil
}

func preflightViewport(size, orientation string) (int, int) {
	width, height, ok := pageSizePoints(size)
	if !ok {
		width, height, _ = pageSizePoints("A4")
	}
	if strings.EqualFold(orientation, "landscape") {
		width, height = height, width
	}
	return int(math.Round(width / 72 * 96)), int(math.Round(height / 72 * 96))
}

func checkPDFInfo(ctx context.Context, pdfFile string, options PreflightOptions) []PreflightCheck {
	output, err := runPreflightTool(ctx, options.PDFInfoBinary, "pdfinfo", pdfFile)
	if err != nil {
		return []PreflightCheck{failedCheck("pdf-pages", err.Error()), failedCheck("pdf-page-size", err.Error())}
	}
	pages, width, height, err := parsePDFInfo(output)
	if err != nil {
		return []PreflightCheck{failedCheck("pdf-pages", err.Error()), failedCheck("pdf-page-size", err.Error())}
	}
	pageCheck := passedCheck("pdf-pages", fmt.Sprintf("%d pages", pages))
	if options.ExpectedPages > 0 && pages != options.ExpectedPages {
		pageCheck = failedCheck("pdf-pages", fmt.Sprintf("got %d pages, expected %d", pages, options.ExpectedPages))
	}
	sizeCheck := passedCheck("pdf-page-size", fmt.Sprintf("%.2f x %.2f pt", width, height))
	if options.ExpectedPageSize != "" {
		expectedWidth, expectedHeight, ok := pageSizePoints(options.ExpectedPageSize)
		if !ok {
			sizeCheck = failedCheck("pdf-page-size", "unknown expected size "+options.ExpectedPageSize)
		} else {
			if strings.EqualFold(options.ExpectedOrientation, "landscape") {
				expectedWidth, expectedHeight = expectedHeight, expectedWidth
			}
			if math.Abs(width-expectedWidth) > 2 || math.Abs(height-expectedHeight) > 2 {
				sizeCheck = failedCheck("pdf-page-size", fmt.Sprintf("got %.2f x %.2f pt, expected %s %s (%.2f x %.2f pt)", width, height, options.ExpectedPageSize, options.ExpectedOrientation, expectedWidth, expectedHeight))
			}
		}
	}
	return []PreflightCheck{pageCheck, sizeCheck}
}

func parsePDFInfo(output string) (int, float64, float64, error) {
	pageMatch := regexp.MustCompile(`(?m)^Pages:\s+(\d+)\s*$`).FindStringSubmatch(output)
	sizeMatch := regexp.MustCompile(`(?m)^Page size:\s+([0-9.]+)\s+x\s+([0-9.]+)\s+pts`).FindStringSubmatch(output)
	if pageMatch == nil || sizeMatch == nil {
		return 0, 0, 0, fmt.Errorf("pdfinfo output is missing Pages or Page size")
	}
	pages, _ := strconv.Atoi(pageMatch[1])
	width, _ := strconv.ParseFloat(sizeMatch[1], 64)
	height, _ := strconv.ParseFloat(sizeMatch[2], 64)
	return pages, width, height, nil
}

func checkPDFFonts(ctx context.Context, pdfFile string, options PreflightOptions) PreflightCheck {
	output, err := runPreflightTool(ctx, options.PDFFontsBinary, "pdffonts", pdfFile)
	if err != nil {
		return failedCheck("pdf-fonts", err.Error())
	}
	fonts, unembedded := parsePDFFonts(output)
	if fonts == 0 {
		return failedCheck("pdf-fonts", "pdffonts reported no fonts")
	}
	if len(unembedded) > 0 {
		return failedCheck("pdf-fonts", "unembedded fonts: "+strings.Join(unembedded, ", "))
	}
	return passedCheck("pdf-fonts", fmt.Sprintf("%d fonts embedded", fonts))
}

func parsePDFFonts(output string) (int, []string) {
	lineRe := regexp.MustCompile(`\s+(yes|no)\s+(yes|no)\s+(yes|no)\s+\d+\s+\d+\s*$`)
	fonts := 0
	unembedded := make([]string, 0)
	for _, line := range strings.Split(output, "\n") {
		match := lineRe.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		fonts++
		if match[1] != "yes" {
			name := strings.Fields(line)
			if len(name) > 0 {
				unembedded = append(unembedded, name[0])
			}
		}
	}
	return fonts, unembedded
}

func checkPDFRawTeX(ctx context.Context, pdfFile string, options PreflightOptions) PreflightCheck {
	output, err := runPreflightTool(ctx, options.PDFToTextBinary, "pdftotext", pdfFile, "-")
	if err != nil {
		return failedCheck("pdf-raw-tex", err.Error())
	}
	markers := rawTeXMarkers(output)
	if len(markers) > 0 {
		return failedCheck("pdf-raw-tex", "raw TeX markers in extracted text: "+strings.Join(markers, ", "))
	}
	return passedCheck("pdf-raw-tex", "no raw TeX markers in extracted text")
}

func runPreflightTool(ctx context.Context, configured, name string, args ...string) (string, error) {
	binary := configured
	if binary == "" {
		var err error
		binary, err = exec.LookPath(name)
		if err != nil {
			return "", fmt.Errorf("%s not found; install Poppler or configure its binary path", name)
		}
	}
	output, err := exec.CommandContext(ctx, binary, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s failed: %w: %s", name, err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func pageSizePoints(size string) (float64, float64, bool) {
	dimensions := map[string][2]float64{
		"a3": {841.89, 1190.55}, "a4": {595.28, 841.89}, "a5": {419.53, 595.28},
		"b4": {708.66, 1000.63}, "b5": {498.90, 708.66}, "letter": {612, 792}, "legal": {612, 1008},
	}
	dimension, ok := dimensions[strings.ToLower(strings.TrimSpace(size))]
	return dimension[0], dimension[1], ok
}

func passedCheck(name, detail string) PreflightCheck {
	return PreflightCheck{Name: name, Passed: true, Detail: detail}
}

func failedCheck(name, detail string) PreflightCheck {
	return PreflightCheck{Name: name, Passed: false, Detail: detail}
}
