package renderer

import (
	"embed"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
)

const katexVersion = "0.18.1"

//go:embed assets/katex/katex.min.css assets/katex/katex.min.js assets/katex/fonts/*.woff2
var katexAssets embed.FS

var katexFontSourceRe = regexp.MustCompile(`url\(fonts/([A-Za-z0-9_-]+\.woff2)\) format\("woff2"\),url\(fonts/[A-Za-z0-9_-]+\.woff\) format\("woff"\),url\(fonts/[A-Za-z0-9_-]+\.ttf\) format\("truetype"\)`)
var embeddedKaTeXRuntime = mustBuildKaTeXRuntime()

const katexBootstrap = `(function(){function renderMath(){document.querySelectorAll("[data-katex]:not([data-katex-rendered])").forEach(function(element){var expression=element.getAttribute("data-katex")||"";var displayMode=element.classList.contains("katex-display");element.classList.remove("katex");element.classList.remove("katex-display");katex.render(expression,element,{displayMode:displayMode,throwOnError:false,output:"htmlAndMathml",strict:"warn"});element.setAttribute("data-katex-rendered","true")})}if(document.readyState==="loading"){document.addEventListener("DOMContentLoaded",renderMath,{once:true})}else{renderMath()}})();`

func mustBuildKaTeXRuntime() string {
	cssBytes, err := katexAssets.ReadFile("assets/katex/katex.min.css")
	if err != nil {
		panic(fmt.Errorf("read embedded KaTeX CSS: %w", err))
	}
	jsBytes, err := katexAssets.ReadFile("assets/katex/katex.min.js")
	if err != nil {
		panic(fmt.Errorf("read embedded KaTeX JavaScript: %w", err))
	}
	if strings.Contains(string(jsBytes), "</script") {
		panic("embedded KaTeX JavaScript contains a closing script tag")
	}

	css := string(cssBytes)
	fonts := katexFontSourceRe.FindAllStringSubmatch(css, -1)
	if len(fonts) == 0 {
		panic("embedded KaTeX CSS contains no recognized WOFF2 font sources")
	}
	css = katexFontSourceRe.ReplaceAllStringFunc(css, func(source string) string {
		match := katexFontSourceRe.FindStringSubmatch(source)
		font, err := katexAssets.ReadFile("assets/katex/fonts/" + match[1])
		if err != nil {
			panic(fmt.Errorf("read embedded KaTeX font %s: %w", match[1], err))
		}
		encoded := base64.StdEncoding.EncodeToString(font)
		return `url("data:font/woff2;base64,` + encoded + `") format("woff2")`
	})
	if strings.Contains(css, "url(fonts/") {
		panic("embedded KaTeX CSS contains unresolved font paths")
	}

	return `<style id="karte-katex-css" data-katex-version="` + katexVersion + `">` + css + `</style>` +
		`<script id="karte-katex-library" data-katex-version="` + katexVersion + `">` + string(jsBytes) + `</script>` +
		`<script id="karte-katex-bootstrap">` + katexBootstrap + `</script>`
}

func katexRuntimeFor(content string) string {
	if !strings.Contains(content, "data-katex=") {
		return ""
	}
	return embeddedKaTeXRuntime
}

func injectKaTeXRuntime(document, runtime string) string {
	if runtime == "" {
		return document
	}
	lower := strings.ToLower(document)
	if at := strings.Index(lower, "</head>"); at >= 0 {
		return document[:at] + runtime + document[at:]
	}
	return runtime + document
}
