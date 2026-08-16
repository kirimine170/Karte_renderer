# Karte activity report fixture

This package turns a representative activity report into a reusable Karte
Format fixture. The normal Markdown document and the Marp deck share the same
palette, type scale, surface treatment, tables, and deterministic progress
graphic.

The package is self-contained and uses system fonts. It is intended to test
package resolution and rendering without network access or licensed assets.

## Contents

- `fixtures/activity-report.md`: an A4 report with a summary, KPI cards,
  progress chart, delivery table, and next actions.
- `fixtures/activity-report.marp.md`: the same information arranged as a 16:9
  presentation.
- `assets/progress.svg`: the shared deterministic progress graphic.
- `markdown/`: the document layout and ordered screen/print styles.
- `marp/`: the corresponding Marp theme.
- `expected.json`: stable package resolution and rendered-content expectations.

The fixture tests validate the manifest paths, resolver output, shared design
tokens, local assets, normal-document HTML and PDF, and Marp HTML, PDF, and
PPTX. The real-output test stages the package without changing its relative
paths, then exercises the public `ConvertFile` API with the existing explicit
CSS and Marp theme options.
