# Karte Renderer

`karte_renderer` is a standalone Go package that collects the rendering-oriented parts of [kirimine170/Karte](https://github.com/kirimine170/Karte): Markdown rendering, Marp-style slide rendering, `@import` expansion, front matter extraction, KaTeX placeholder rendering, layout wrapping, and PDF export through `wkhtmltopdf`.

## Features

- Render Markdown strings or files to HTML.
- Parse YAML-like front matter (`title`, `marp`, `theme`, `layout`, `owners`, `viewers`, and arbitrary keys in `Data`).
- Expand `@import(type="csv" path="...")` and `@import(type="md" path="...")` directives inside a safe project root.
- Render Marp decks when `marp: true` is present, splitting slides on `---` lines and preserving `_class` directives as section classes.
- Convert `$...$` and `$$$...$$$` math into KaTeX-compatible HTML placeholders while leaving code spans/blocks untouched.
- Wrap output with `themes/default/preview.html`, then `themes/default/layout.html`, then a built-in fallback layout.
- Export rendered HTML to PDF via `wkhtmltopdf` with local file access enabled.

## Usage

```go
html, fm, err := renderer.RenderMarkdown(projectRoot, "content/page.md")
```

```go
html, fm, err := renderer.RenderString(projectRoot, "---\ntitle: Deck\nmarp: true\n---\n# Slide")
```

```go
err := renderer.ExportPDF("/path/to/page.html", "/path/to/page.pdf")
```
