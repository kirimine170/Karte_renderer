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
that normalize outside the package are invalid. Filesystem resolution must also
reject symlinks that escape the package before a referenced file is used.

Remote URLs, registries, downloads, and archive packages are outside v0.1.

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
