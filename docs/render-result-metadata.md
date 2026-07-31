# RenderResult metadata contract

`RenderResult` is the additive metadata API for consumers that need more than
the rendered HTML and parsed front matter. The existing `RenderMarkdown`,
`RenderString`, and `ConvertFile` signatures remain supported.

## Contract

`RenderMetadata.SchemaVersion` is `1`. A consumer must reject a higher schema
version that it does not understand. New optional fields may be added without
incrementing the version; changing field meaning or removing a field requires
a new version.

```go
result, err := renderer.RenderMarkdownResult(projectRoot, "content/page.md")
if err != nil {
    // Fatal input, import, parsing, or rendering failure.
}

fmt.Println(result.HTML)
fmt.Println(result.Metadata.FrontMatter.Title)
```

The metadata fields have these semantics:

- `frontMatter` preserves the typed Karte fields and the complete YAML map.
  Typed fields use lower-camel JSON names; custom and nested YAML remains under
  `frontMatter.data`.
- `dependencies` contains source, Markdown/CSV/TeX imports, and a custom
  layout when one is used. Paths are project-root-relative and use `/` on all
  operating systems.
- `links` contains Markdown links in source order. Internal targets include a
  normalized project-relative `path`; URL query and fragment parts remain in
  `target`. `target` preserves the source spelling while link classification
  uses the CommonMark-normalized destination. A query-only target resolves to
  the entry document path when rendering a file.
- `assets` contains Markdown images in source order and classifies them as
  `available`, `missing`, `external`, `embedded`, `unresolved`, `unavailable`,
  or `outside-root`. `unresolved` means that a valid reference has no
  filesystem path, such as an empty, query-only, or fragment-only destination.
  `unavailable` means that the filesystem could not inspect the resolved path
  for a reason other than nonexistence.
- `diagnostics` contains non-fatal findings. A missing or outside-root asset is
  a warning because the HTML render can still be useful. Fatal parse, import,
  path, and renderer failures continue to be returned as `error`.

Dependencies are de-duplicated by `(kind, path)` while preserving first-use
order. Links and assets are not de-duplicated because their source occurrence
order is meaningful to callers.

## Path and security rules

Local paths are resolved from the entry document directory. A leading `/`
means the configured project root. Query strings and fragments do not
participate in filesystem resolution. CommonMark backslash escapes, numeric
references, and named HTML entities are normalized with the same Goldmark
rules used by HTML rendering before path resolution. Lexical traversal and
symlink escape are reported as `outside-root`; the collector never reads an
asset outside the trusted root.

The metadata collector does not fetch external URLs. `external` therefore
means externally addressed, not network-verified.

## Compatibility scope

Version 1 covers Markdown links and images plus dependencies already consumed
by the renderer. HTML tags embedded in raw Markdown and assets referenced only
from CSS are not collected yet. Those sources can be added as optional metadata
in a compatible release, provided existing field meanings and ordering remain
unchanged.
