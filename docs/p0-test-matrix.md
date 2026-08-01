# P0 test matrix

The P0 acceptance suite maps the spreadsheet tasks to executable regression evidence.

| Task | Coverage |
| --- | --- |
| T-008 | `printout_test.go` and the combined B5 fixture in `p0_acceptance_test.go` verify page size, mirrored margins, running text, folios, chapter starts, and option precedence. |
| T-031 | `renderer_test.go` verifies multiline KaTeX across lists, blockquotes, comments, raw HTML, and code; the combined fixture verifies it alongside pagination and figures. |
| T-032 | `pagination_test.go` verifies explicit breaks, keep rules, CRLF, and literal containers; the combined fixture verifies the emitted print structure. |
| T-033 | `chart_test.go` verifies supported chart types, deterministic SVG, escaping, invalid data, Marp HTML mode, and duplicate bar keys. |
| T-034 | `figures_test.go` verifies numbering, captions, sources, notes, forward references, imported assets, and invalid identifiers. |
| T-035 | `preflight_test.go` verifies report checks and failures; `TestP0Acceptance48PageFixtureContract` fixes the B5/48-page fixture contract; CI performs the real Chrome/Poppler preflight. |

GitHub Actions runs Go tests on Linux, macOS, and Windows, Node fixture tests on Linux, formatting and vet checks, and a real 48-page B5 PDF preflight on Linux.
