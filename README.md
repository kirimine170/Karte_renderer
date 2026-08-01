# Karte Renderer

`KarteRenderer` is the rendering-focused Go module extracted from
[`kirimine170/Karte`](https://github.com/kirimine170/Karte). It provides a
library API and the `karte-renderer` CLI for normal Markdown documents and
Marp slide decks.

## Supported conversions

| Input | HTML | PDF | PPTX |
| --- | --- | --- | --- |
| Markdown | built-in | Chromium, with `wkhtmltopdf` fallback | - |
| Marp Markdown (`marp: true`) | official Marp CLI | official Marp CLI | official Marp CLI |

The split is intentional:

- Normal Markdown uses the CommonMark-compliant
  [goldmark](https://github.com/yuin/goldmark) renderer with GFM tables,
  strikethrough, task lists, autolinks, footnotes, and definition lists.
- Marp output delegates to the
  [official Marp CLI](https://github.com/marp-team/marp-cli), so themes,
  directives, speaker notes, PDF, and PowerPoint output behave like Marp
  rather than a partial reimplementation.
- Normal document PDF output prefers a Chromium-family browser for modern CSS
  support. Existing `wkhtmltopdf` integrations remain supported.

Marp's normal PPTX output prioritizes visual fidelity and stores rendered
slides as images. `--pptx-editable` is experimental, needs LibreOffice as well
as a browser, does not preserve every complex style, and does not support
presenter notes. See the
[Marp CLI documentation](https://github.com/marp-team/marp-cli#convert-to-powerpoint-document-pptx-).

## Features

- CommonMark and GitHub Flavored Markdown rendering.
- Real YAML front matter, including lists, booleans, numbers, and nested data.
- Karte-compatible fields: `title`, `marp`, `theme`, `layout`, `owners`,
  `viewers`, and typed `printout`; all metadata is also available through
  `FrontMatter.Data`.
- Root-scoped Markdown, CSV, and TeX `@import` directives, including nested
  import expansion, cycle detection, selected CSV columns, inline or display
  TeX placeholders, and symlink escape protection.
- Offline KaTeX rendering for `$...$`, `$$$...$$$`, and imported TeX, with the
  JavaScript, CSS, and WOFF2 fonts embedded only when a document contains math.
- Project layouts in `themes/default/preview.html` or
  `themes/default/layout.html`, with a printable standalone fallback layout.
- A built-in Purple Color Palette stylesheet for normal Markdown documents,
  with explicit custom CSS and no-CSS modes.
- A4 portrait defaults plus validated A3/A4/A5/B4/B5/Letter/Legal overrides,
  mirrored book margins, running header/footer, page-number folios, and
  right-page chapter starts for normal-document PDF output.
- Local asset resolution when HTML is written to a different output directory.
- Context-aware Go APIs, atomic HTML writes, actionable external-tool errors,
  and a dependency doctor.

## Requirements

- Go 1.22 or newer.
- Node.js 18 or newer plus `npm ci` for Marp output.
- Google Chrome, Chromium, Microsoft Edge, or Brave for normal document PDF.
- A Marp-supported browser for Marp PDF/PPTX. Chrome works for both pipelines.
- LibreOffice only when using experimental editable PPTX output.

Install and build:

```sh
npm ci
go build -o bin/karte-renderer ./cmd/karte-renderer
bin/karte-renderer doctor
```

`package-lock.json` pins the Marp toolchain. HTML conversion for normal
Markdown does not require Node.js or a browser.

Math-enabled HTML remains standalone: the renderer embeds KaTeX and its fonts
directly into the generated document, so previews and PDFs work offline.

## CLI

```sh
# Normal Markdown
bin/karte-renderer document.md output/document.html
bin/karte-renderer document.md output/document.pdf

# Marp, selected by `marp: true` front matter
bin/karte-renderer slides.md output/slides.html
bin/karte-renderer --allow-local-files slides.md output/slides.pdf
bin/karte-renderer --allow-local-files slides.md output/slides.pptx
```

Common options:

```text
--root PATH             trusted project root for themes and @import
--hardwrap              turn Markdown soft breaks into <br>
--css PATH              replace the built-in document CSS with this file
--no-css                disable CSS for normal Markdown documents
--theme NAME_OR_CSS     override the Marp theme
--theme-set CSS         add Marp theme CSS; repeatable
--allow-local-files     allow trusted local assets in Marp browser exports
--html                  allow trusted raw HTML in Marp (needed for CSV imports)
--browser-path PATH     explicitly choose Marp's browser
--pptx-editable         request Marp's experimental editable PPTX
--pdf-engine ENGINE     auto, chromium, or wkhtmltopdf for document PDF
--pdf-binary PATH       explicitly choose the document PDF executable
--pdf-page-size SIZE    A3, A4, A5, B4, B5, Letter, or Legal
--pdf-orientation MODE  portrait or landscape
--pdf-margin LENGTHS    one to four physical page-margin lengths
--pdf-inside-margin N   binding-edge margin for mirrored pages
--pdf-outside-margin N  outside-edge margin for mirrored pages
--pdf-header TEXT       running page header
--pdf-footer TEXT       running page footer
--pdf-page-numbers      show outside-edge page-number folios (or =false)
--pdf-chapter-start N   right or any
```

The environment variables `MARP_BINARY` and `KARTE_PDF_BINARY` are also
supported. The CLI looks for a project-local `node_modules/.bin/marp` before
reporting a missing Marp installation.

## Go API

Render an embedded Markdown document:

```go
html, frontMatter, err := renderer.RenderMarkdown(projectRoot, "content/page.md")
```

Convert a file based on its metadata and output extension:

```go
frontMatter, err := renderer.ConvertFile(ctx, "slides.md", "build/slides.pptx", renderer.ConvertOptions{
    Root: projectRoot,
    Marp: renderer.MarpOptions{
        AllowLocalFiles: true,
    },
})
```

For a B5 book, Go callers can override the same fields individually:

```go
pageNumbers := true
frontMatter, err := renderer.ConvertFile(ctx, "book.md", "build/book.pdf", renderer.ConvertOptions{
    Root: projectRoot,
    PDF: renderer.PDFOptions{
        PageSize:      "B5",
        Orientation:   "portrait",
        Margin:        "18mm 15mm",
        InsideMargin:  "20mm",
        OutsideMargin: "14mm",
        Header:        "Book title",
        PageNumbers:   &pageNumbers,
        ChapterStart:  "right",
    },
})
```

## Book printout front matter

```yaml
printout:
  size: B5
  orientation: portrait
  margin: 18mm 15mm
  insideMargin: 20mm
  outsideMargin: 14mm
  header: Book title
  footer: Chapter title
  pageNumbers: true
  chapterStart: right
```

`printout: B5` is available as a shorthand. Explicit CLI/Go fields override
only their matching front-matter fields. The renderer emits the settings as a
late paged-media override, so screen styling is unchanged; mirrored `:left`
and `:right` rules put the inside margin on the binding edge.

### Page breaks and keep rules

Use a standalone directive for a deterministic page boundary:

```md
@pagebreak
@pagebreak(before)
@pagebreak(after)
```

The plain form is an alias for `before`; directives in fenced code blocks stay
literal. Print CSS also keeps headings with their following block and avoids
splitting figures, captions, images, tables, code blocks, blockquotes, and
display equations when the paged-media engine can honor the constraint.
Trusted HTML/templates may use `.karte-break-before`, `.karte-break-after`,
`.karte-keep-with-next`, or `.karte-keep-together` directly.

### Deterministic CSV charts

`@chart` generates an inline, responsive SVG during rendering. It does not
load JavaScript, fonts, or chart services at runtime:

```md
@chart(type="scatter" path="data/performances.csv" x="attendance" y="profit" series="venue" title="Attendance and profit" xLabel="Attendance" xUnit="people" yLabel="Profit" yUnit="JPY" note="Source: performance log")
```

The initial chart types are `scatter`, `line`, `bar`, and `histogram`.
`width`/`height` set the SVG view box and `bins` controls a histogram. A
`series` column produces a stable legend with monochrome-safe marker shapes and
dash patterns. CSV paths use the same project-root and symlink safety boundary
as imports, and invalid or missing numeric values fail the conversion with a
row/column error.

The lower-level `ExportMarp` and `ExportHTMLPDF` functions are available when
the caller already owns the Markdown/HTML pipeline. The legacy
`ExportPDFWithBinary` function remains a direct `wkhtmltopdf` compatibility
API.

## Project layouts and imports

`RenderMarkdown` searches in this order:

1. `themes/default/preview.html`
2. `themes/default/layout.html`
3. the built-in standalone layout

Layouts may contain `{{TITLE}}`, `{{CSS}}`, `{{KATEX}}`, and `{{CONTENT}}`
placeholders.
`{{CSS}}` is replaced by a complete `<style>` element. It contains the bundled
`assets/default.css` by default, the file supplied with `--css PATH` when set,
or nothing when `--no-css` is set. Relative `--css` paths are resolved from the
current working directory. These options affect normal Markdown documents;
Marp continues to use its own theme options.
`{{KATEX}}` is populated only when math is present. Layouts without that
placeholder receive the runtime automatically before `</head>`.

```md
@import(type="md" path="partials/intro.md")
@import(type="csv" path="data/results.csv" select="Name,Score")
@import(type="tex" path="math/model.tex" display="block")
```

Paths are resolved relative to the current Markdown file and must stay below
the configured project root. TeX imports default to display mode and also
accept `display="inline"`. Marp CSV and TeX imports produce HTML, so pass
`--html` only for trusted input.

A minimal multi-file example is available in `examples/karte-format-basic`.
The more involved fixture in `testdata/karte-format/complex` combines nested
Markdown imports, CSV projections, TeX modes, and a WebP asset.

## Security notes

- `RenderMarkdown` preserves raw HTML for compatibility with Karte templates;
  treat rendered Markdown as trusted or sanitize the resulting HTML before
  serving untrusted user content.
- Marp blocks local file access by default. `--allow-local-files` deliberately
  follows Marp's security model and should only be used for trusted decks.
- `@import` rejects lexical path traversal, symlink escapes, cycles, and import
  nesting deeper than 32 levels.

## Development

```sh
go test ./...
go vet ./...
npm test

# End-to-end samples
go run ./cmd/karte-renderer examples/document.md output/document.pdf
go run ./cmd/karte-renderer examples/slides.md output/slides.pptx
```

The test suite covers GFM, structured YAML, math/code boundaries, layouts,
imports, path safety, book printout precedence and validation, Marp invocation,
PDF-engine invocation, and CLI behavior. The Node tests validate the linked
karte-format fixture resources and render every fixture formula with the pinned
KaTeX release.
