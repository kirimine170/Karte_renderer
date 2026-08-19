package renderer

// PreviewSchemaVersion identifies the stable JSON shape and semantics of
// PreviewResult. Consumers must use this value instead of assuming that the
// zero value is a supported schema.
const PreviewSchemaVersion = 1

// PreviewKind identifies the renderer used for a preview.
type PreviewKind string

const (
	PreviewKindMarkdown PreviewKind = "markdown"
	PreviewKindMarp     PreviewKind = "marp"
)

// PreviewPageMode describes where pagination is determined. It does not imply
// that every PreviewResult has an eagerly populated Pages slice.
type PreviewPageMode string

const (
	PreviewPageContinuous PreviewPageMode = "continuous"
	PreviewPageSlides     PreviewPageMode = "slides"
	PreviewPageRuntime    PreviewPageMode = "runtime-pages"
)

// PreviewSourceRange locates a page in unexpanded source supplied to the
// preview renderer. Byte and UTF-16 offsets are end-exclusive. Lines are
// zero-based and end-exclusive. Path, when present, is root-relative and uses
// slash separators.
type PreviewSourceRange struct {
	Path       string `json:"path,omitempty"`
	StartByte  int    `json:"startByte"`
	EndByte    int    `json:"endByte"`
	StartUTF16 int    `json:"startUtf16"`
	EndUTF16   int    `json:"endUtf16"`
	StartLine  int    `json:"startLine"`
	EndLine    int    `json:"endLine"`
}

// PreviewPage describes one logical preview page. ID is opaque to consumers
// and unique within a PreviewResult. Index is zero-based and follows source
// order. Editable is true only when Source maps exactly to writable source.
type PreviewPage struct {
	ID       string              `json:"id"`
	Index    int                 `json:"index"`
	Editable bool                `json:"editable"`
	Source   *PreviewSourceRange `json:"source,omitempty"`
}

// PreviewResult is the additive, versioned preview contract consumed by Karte.
// Producers set SchemaVersion to PreviewSchemaVersion. Consumers should ignore
// unknown JSON fields after checking that the schema version is supported.
type PreviewResult struct {
	SchemaVersion int                `json:"schemaVersion"`
	Kind          PreviewKind        `json:"kind"`
	PageMode      PreviewPageMode    `json:"pageMode"`
	HTML          string             `json:"html"`
	Pages         []PreviewPage      `json:"pages,omitempty"`
	Metadata      RenderMetadata     `json:"metadata"`
	Diagnostics   []RenderDiagnostic `json:"diagnostics,omitempty"`
}

// PreviewOptions configures a context-aware preview render. Path is the
// root-relative, slash-separated path of source, when source represents a
// project file.
type PreviewOptions struct {
	Path        string
	HardWrap    bool
	TrustedHTML bool
	Theme       string
	ThemeSet    []string
}
