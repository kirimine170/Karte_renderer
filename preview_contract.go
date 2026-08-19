package renderer

// PreviewSchemaVersion identifies the stable JSON shape and semantics of
// PreviewResult.
const PreviewSchemaVersion = 1

// PreviewKind identifies the renderer used for a preview.
type PreviewKind string

const (
	PreviewKindMarkdown PreviewKind = "markdown"
	PreviewKindMarp     PreviewKind = "marp"
)

// PreviewPageMode describes how callers should interpret preview pages.
type PreviewPageMode string

const (
	PreviewPageContinuous PreviewPageMode = "continuous"
	PreviewPageSlides     PreviewPageMode = "slides"
	PreviewPageRuntime    PreviewPageMode = "runtime-pages"
)

// PreviewSourceRange locates a page in the original UTF-8 source. Byte and
// UTF-16 offsets are end-exclusive. Lines are zero-based and end-exclusive.
type PreviewSourceRange struct {
	Path       string `json:"path,omitempty"`
	StartByte  int    `json:"startByte"`
	EndByte    int    `json:"endByte"`
	StartUTF16 int    `json:"startUtf16"`
	EndUTF16   int    `json:"endUtf16"`
	StartLine  int    `json:"startLine"`
	EndLine    int    `json:"endLine"`
}

// PreviewPage describes one logical preview page.
type PreviewPage struct {
	ID       string              `json:"id"`
	Index    int                 `json:"index"`
	Editable bool                `json:"editable"`
	Source   *PreviewSourceRange `json:"source,omitempty"`
}

// PreviewResult is the additive preview contract consumed by Karte.
type PreviewResult struct {
	SchemaVersion int                `json:"schemaVersion"`
	Kind          PreviewKind        `json:"kind"`
	PageMode      PreviewPageMode    `json:"pageMode"`
	HTML          string             `json:"html"`
	Pages         []PreviewPage      `json:"pages,omitempty"`
	Metadata      RenderMetadata     `json:"metadata"`
	Diagnostics   []RenderDiagnostic `json:"diagnostics,omitempty"`
}

// PreviewOptions configures a context-aware preview render.
type PreviewOptions struct {
	Path        string
	HardWrap    bool
	TrustedHTML bool
	Theme       string
	ThemeSet    []string
}
