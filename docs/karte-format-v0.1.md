# Karte Format package manifest v0.1

Karte Format v0.1 is a local directory contract that groups normal Markdown
layout and styles, Marp themes, and shared assets. The fixed manifest filename
is `karte-format.yaml`.

```text
karte-default/
├── karte-format.yaml
├── markdown/
│   ├── layout.html
│   ├── base.css
│   └── print.css
├── marp/
│   └── karte.css
└── assets/
```

## Manifest

```yaml
schemaVersion: 1
name: karte-default
version: 1.0.0

markdown:
  layout: markdown/layout.html
  styles:
    - markdown/base.css
    - markdown/print.css

marp:
  defaultTheme: karte
  themes:
    - marp/karte.css

assets:
  directory: assets
```

All fields shown above are required in v0.1. Unknown fields are errors.

| Field | Contract |
| --- | --- |
| `schemaVersion` | Integer `1`. Readers must reject unsupported versions. |
| `name` | Stable lowercase package identifier. |
| `version` | Semantic package version such as `1.0.0`. |
| `markdown.layout` | HTML layout containing renderer placeholders. |
| `markdown.styles` | Ordered, non-empty list of document stylesheets. |
| `marp.defaultTheme` | Theme name selected when no stronger setting exists. |
| `marp.themes` | Ordered, non-empty list of Marp theme CSS files. |
| `assets.directory` | Shared root for package fonts, images, and other assets. |

`markdown.styles` order is significant: later styles may override earlier
styles. Theme files are registered in manifest order.

## Paths and security

Manifest paths use `/` separators and are relative to the package directory.
Absolute paths, URLs, Windows drive paths, backslashes, empty paths, and paths
that normalize outside the package are invalid. Paths must already be canonical:
`.` segments, empty segments, and trailing slashes are not accepted. Filesystem
resolution must also reject symlinks that escape the package before a referenced
file is used.

Remote URLs, registries, downloads, and archive packages are outside v0.1.

## Filesystem and theme asset resolution

`ResolveFormatPackageAssets` is the v0.1 filesystem contract. It validates the
manifest and returns canonical host paths for the declared layout, ordered
Markdown styles, ordered Marp themes, and asset directory. It also discovers
`url(...)` references in those styles and themes without applying the package
to either conversion pipeline. Its ordered `Distribution` list contains the
manifest, declared files, and each distinct local asset exactly once; embedded
references are absent. `NewFormatAssetResolver` exposes the same path boundary
for callers that need to validate an individual package member.

Declared manifest paths are relative to the format package root. The layout,
styles, and themes must exist as regular files; `assets.directory` must exist
as a directory. A missing path and a file/directory mismatch are distinct
diagnostics.

A local URL in Markdown CSS or Marp theme CSS is relative to the CSS file that
contains it, following normal CSS URL semantics. For example, both
`markdown/base.css` and `marp/karte.css` can refer to the shared font as
`url("../assets/fonts/report.woff2")`. `..` is allowed in this CSS-relative
form only when the normalized result remains inside the package. Every local
theme URL must resolve to a regular file below `assets.directory`; keeping
theme files below that directory is not sufficient.

Resolution checks both the lexical path and every resolved symlink component.
A symlink may point to another member of the same package, but a symlink to an
outside file or directory is rejected, including when the final referenced
file does not exist. These checks apply before a file is read or distributed.

Resolver failures expose a stable `FormatResolverError.Code`:

| Code | Meaning |
| --- | --- |
| `invalid_path` | Empty, malformed, NUL-containing, or non-canonical package path. |
| `absolute_path` | POSIX absolute, protocol-relative, Windows drive, or UNC path. |
| `uri_scheme` | Non-embedded URI scheme such as `https:` or `file:`. |
| `outside_package` | Lexical traversal leaves the package root. |
| `outside_assets_directory` | A local CSS URL is inside the package but outside `assets.directory`. |
| `symlink_escape` | An existing or missing target escapes through a symlink. |
| `path_not_found` | A path is safely inside the package but does not exist. |
| `expected_file` | A required regular file resolved to another filesystem type. |
| `expected_directory` | A required directory resolved to another filesystem type. |

The syntax checks are host-independent. In particular, `C:/...`, `C:\\...`,
`\\\\server\\share\\...`, and `file:///C:/...` are rejected on macOS and Linux
as well as on Windows. Percent-encoded traversal and absolute separators are
decoded before the package boundary is checked.

## Fonts, embedding, and distribution

URLs inside an `@font-face` block are font references. Outside such a block,
the `.woff`, `.woff2`, `.ttf`, `.otf`, and `.eot` extensions also identify a
font; other local URLs are image assets in v0.1. `local(...)` font fallbacks do
not name package files and are not discovered. Package authors should include
a file-backed `url(...)` source when reproducible output must not depend on
fonts installed on the rendering host.

`data:` URLs and fragment-only URLs such as `url(#filter)` have the `embedded`
disposition. They have no package path, are not copied as separate files, and
do not permit filesystem access. Every other URI scheme is rejected in v0.1;
the resolver never downloads remote fonts or images.

Local references have the `local` disposition and carry their normalized,
slash-separated package path. A distributor must preserve that path beneath
the package root and include the manifest, all declared files, and every
distinct local referenced file. Duplicate occurrences may share one
distributed file. Embedded references add no distribution entry. Copying the
complete package directory satisfies these rules; selective bundlers must not
flatten the asset paths or substitute host-installed fonts.

CSS `@import` references, including its `url(...)` form, are not asset
references and are intentionally not followed by this resolver. Multiple CSS
loading and conversion integration remain separate work so this contract does
not create a second stylesheet application path.

## Precedence

When the package is integrated with conversion, settings use this precedence:

1. Explicit CLI or Go API options.
2. Markdown front matter.
3. Karte Format package defaults.
4. Built-in renderer defaults.

The package must reuse the normal Markdown CSS loader instead of creating a
second CSS application path.

## Compatibility

`schemaVersion` controls the shape and interpretation of the manifest.
`version` identifies a release of one package. Adding a new optional behavior
requires either a backwards-compatible schema extension or a new schema
version; changing an existing field's meaning requires a new schema version.

The machine-readable companion is
[`karte-format-v0.1.schema.json`](karte-format-v0.1.schema.json). A minimal
authoring template is in `examples/karte-format-v0.1`.

## Reference fixture

`examples/karte-format-report` is the representative activity-report fixture.
It contains a normal Markdown report, a five-slide Marp version of the same
content, one deterministic SVG used by both outputs, matching document and
slide design tokens, and `expected.json` for stable resolution and render
expectations. CI stages the package with its relative paths intact and uses the
public conversion API to produce and inspect document HTML/PDF plus Marp
HTML/PDF/PPTX outputs. Unlike the minimal authoring template, this package is
intended to catch visual-contract regressions as the conversion integration
evolves.
