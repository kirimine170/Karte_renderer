# Preview contract v1

Karte_renderer の Preview API は，Karte が HTML 表示，Marp のページ移動，D&D 挿入位置の決定を行うための versioned contract である．Preview API は既存の `RenderMarkdown`，`RenderString`，`RenderResult`，`ConvertFile`，`ExportMarp` へ追加されるものであり，それらの wire format や挙動を変更しない．

## Go API と lifecycle

Preview は Node process の所有期間を明示する `Previewer` を通して生成する．

```go
previewer, err := renderer.NewPreviewer(options)
if err != nil {
    return err
}
defer previewer.Close()

result, err := previewer.Render(ctx, root, source, renderer.PreviewOptions{
    Path: "slides/talk.md",
})
```

公開 API の基本形は次のとおりである．

```go
func NewPreviewer(options PreviewerOptions) (*Previewer, error)
func (p *Previewer) Render(
    ctx context.Context,
    root string,
    source string,
    options PreviewOptions,
) (PreviewResult, error)
func (p *Previewer) Close() error
```

`Previewer` の lifecycle guarantee は次のとおりである．

- Marp の warm render は，同じ `Previewer` が所有する Node process を再利用する．
- 通常 Markdown の render は Node process を lazy start しない．
- request と response は request ID で対応付ける．
- cancel された caller は速やかに復帰し，その request の late response は後続 request の結果として使わない．
- process crash は actionable error を返す．後続 render での再起動可否は error と実装状態に従う．
- `Close` は所有する process を終了し，完了後の `Render` は error を返す．
- Preview HTML の生成に Chromium を使用しない．

Karte は debounce，context cancellation の発行，latest-wins 制御，現在ページの保持，ページ操作，D&D 後の保存，iframe または WebView の状態を所有する．Renderer はこれらの UI state を所有しない．

## PreviewResult

`PreviewResult` の Go contract は次のとおりである．

```go
type PreviewResult struct {
    SchemaVersion int                `json:"schemaVersion"`
    Kind          PreviewKind        `json:"kind"`
    PageMode      PreviewPageMode    `json:"pageMode"`
    HTML          string             `json:"html"`
    Pages         []PreviewPage      `json:"pages,omitempty"`
    Metadata      RenderMetadata     `json:"metadata"`
    Diagnostics   []RenderDiagnostic `json:"diagnostics,omitempty"`
}
```

producer は `SchemaVersion` に `PreviewSchemaVersion` を設定する．v1 の値は `1` である．`RenderMetadata.SchemaVersion` は独立した schema version であり，Preview schema version の代用にはならない．

consumer は，対応する `schemaVersion` を先に確認し，同じ version の未知 field を無視する．field の追加など後方互換な拡張は v1 のまま行える．既存 field の削除，型変更，座標系変更，既存 enum 値の意味変更には Preview schema version の更新が必要である．

`pages` と `diagnostics` は空の場合に JSON から省略される．consumer は missing と空配列を同じものとして扱う．`metadata` は常に存在する．`PreviewResult.Diagnostics` は Preview engine，source mapping，process lifecycle に関する問題を表し，`Metadata.Diagnostics` は既存 `RenderMetadata` が収集する文書 resource の問題を引き続き表す．

例を示す．

```json
{
  "schemaVersion": 1,
  "kind": "marp",
  "pageMode": "slides",
  "html": "<section data-marpit-svg><h1>Preview</h1></section>",
  "pages": [
    {
      "id": "karte-intro",
      "index": 0,
      "editable": true,
      "source": {
        "path": "slides/intro.md",
        "startByte": 20,
        "endByte": 43,
        "startUtf16": 20,
        "endUtf16": 41,
        "startLine": 3,
        "endLine": 5
      }
    }
  ],
  "metadata": {
    "schemaVersion": 1,
    "frontMatter": {
      "title": "Preview contract",
      "marp": true
    }
  }
}
```

## kind と pageMode

`kind` は caller の希望ではなく，Renderer が source の front matter を解析して決定する．

- `markdown` は通常 Markdown renderer を使用したことを表す．
- `marp` は公式 Marp Core semantics を使用したことを表す．

`pageMode` は page boundary を誰が確定できるかを表す．

- `continuous` は通常の連続文書である．Renderer は browser layout 前に物理ページ数を確定したふりをしない．通常は `pages` を返さないが，source に明示された logical boundary を将来返す余地がある．
- `slides` は Renderer が source から各 slide を確定できる．`pages` は source 順で全 slide を表し，`index` は `0` から連続する．Marp はこの mode を使用する．
- `runtime-pages` は `karte-print-page` などの DOM layout 後に物理ページが決まる文書である．返された `pages` を物理ページ一覧と見なしてはならず，Karte が DOM 生成後に page 数と範囲を取得する．

未知の `kind` または `pageMode` を受け取った consumer は編集操作を無効化し，連続表示へ安全に fallback することが推奨される．

## source range 座標系

`PreviewSourceRange` は import 展開後の中間文字列ではなく，Preview API の `source` 引数に渡された展開前の UTF-8 source を参照する．全 range は half-open interval である．

| field | 単位と保証 |
| --- | --- |
| `path` | project root 相対 path．separator は `/` であり，absolute path や `..` segment を含まない．in-memory source など path が不明な場合は省略できる． |
| `startByte` | 元の UTF-8 byte sequence の先頭から数えた byte offset．0-based，inclusive． |
| `endByte` | 元の UTF-8 byte sequence の先頭から数えた byte offset．exclusive． |
| `startUtf16` | JavaScript の `selectionStart` と同じ UTF-16 code unit offset．0-based，inclusive．BMP 外の Unicode scalar value は 2 code unit と数える． |
| `endUtf16` | UTF-16 code unit offset．exclusive． |
| `startLine` | range が始まる logical line．0-based，inclusive． |
| `endLine` | range が触れる logical line 集合の末尾．exclusive． |

LF は 1 つの line boundary として扱う．CRLF も 1 つの line boundary として扱うが，byte offset では 2 bytes，UTF-16 offset では 2 code units を占める．`endByte` が次の line の先頭を指す場合，その次の line は `[startLine, endLine)` に含まない．Karte が文字列を slice するときは byte field を Go の UTF-8 source に，UTF-16 field を JavaScript の text control に使い，両者を混在させない．

各 offset は次の不変条件を満たす．

```text
0 <= startByte  <= endByte  <= len(source in UTF-8 bytes)
0 <= startUtf16 <= endUtf16 <= len(source in UTF-16 code units)
0 <= startLine  <= endLine
```

## page ID と selection fallback

`PreviewPage.ID` は consumer にとって opaque string であり，1 回の render 内で一意である．Karte は ID の prefix，hash 長，suffix を解析してはならない．

ID の選択順は次のとおりである．

1. slide に `<!-- karte-page-id: ID -->` があり，ID が `[A-Za-z0-9][A-Za-z0-9._:-]{0,127}` に適合する場合は，その明示 ID を優先する．
2. 明示 ID がない場合は，slide source range の raw UTF-8 bytes の SHA-256 fingerprint と，同一 fingerprint の 1-based ordinal から決定的に生成する．v1 の生成形は `marp-<先頭24 hex>-<ordinal>` である．
3. 明示 ID 同士，または明示 ID と生成 ID が衝突した場合は，source 順に `--2`，`--3` を付けて一意にする．

同一の source bytes と同一順序からは同じ ID が生成される．ただし，空白または改行だけの編集でも fingerprint は変わり得る．同一内容の slide を前へ追加または削除すると，同じ fingerprint を持つ後続 slide の ordinal は変わり得る．したがって，Karte は render 間で ID が見つからない場合，source range overlap を第一 fallback，`index` を第二 fallback として現在ページを復元する．完全に永続する暗黙 ID は保証しない．

## Editable と import

`Editable` は，Karte が `Source` を使って元ファイルへ変更を適用してよいかを表す capability flag である．

- `Editable=true` は `Source` が存在し，`path` と全 offset が編集対象の原文へ正確に対応する場合に限る．
- `Source=nil` の page は必ず `Editable=false` である．
- `Editable=false` でも，正確な read-only mapping を示すため `Source` が存在する場合がある．consumer は `Source != nil` だけを根拠に編集してはならない．
- entry document 内へ正確に対応できる page は range を返せる．
- import 展開に由来し，元ファイルへ正確に対応できない page は `Source` を省略し，`Editable=false` とする．mapping 不能は diagnostic でも通知する．
- 将来 import 元ファイルの mapping が正確に得られる場合は，その root-relative `path` と range を返せる．この場合も root boundary と書き込み権限を検証してから `Editable=true` にする．

## JSON Schema

次の JSON Schema は PreviewResult v1 の wire contract を記述する．同一 schema version 内の additive extension を受け入れるため，各 object は未知 field を許可する．意味上の制約である page ID の一意性，index の連続性，offset の上限，`Source=nil` なら `Editable=false` は producer と contract test が保証する．

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://github.com/kirimine170/Karte_renderer/schema/preview-result-v1.json",
  "title": "Karte Renderer PreviewResult v1",
  "type": "object",
  "required": ["schemaVersion", "kind", "pageMode", "html", "metadata"],
  "properties": {
    "schemaVersion": { "const": 1 },
    "kind": { "type": "string", "enum": ["markdown", "marp"] },
    "pageMode": {
      "type": "string",
      "enum": ["continuous", "slides", "runtime-pages"]
    },
    "html": { "type": "string" },
    "pages": {
      "type": "array",
      "items": { "$ref": "#/$defs/page" }
    },
    "metadata": {
      "type": "object",
      "required": ["schemaVersion", "frontMatter"],
      "properties": {
        "schemaVersion": { "type": "integer", "minimum": 1 },
        "frontMatter": { "type": "object" }
      },
      "additionalProperties": true
    },
    "diagnostics": {
      "type": "array",
      "items": { "$ref": "#/$defs/diagnostic" }
    }
  },
  "$defs": {
    "page": {
      "type": "object",
      "required": ["id", "index", "editable"],
      "properties": {
        "id": { "type": "string", "minLength": 1 },
        "index": { "type": "integer", "minimum": 0 },
        "editable": { "type": "boolean" },
        "source": { "$ref": "#/$defs/sourceRange" }
      },
      "additionalProperties": true
    },
    "sourceRange": {
      "type": "object",
      "required": [
        "startByte",
        "endByte",
        "startUtf16",
        "endUtf16",
        "startLine",
        "endLine"
      ],
      "properties": {
        "path": { "type": "string" },
        "startByte": { "type": "integer", "minimum": 0 },
        "endByte": { "type": "integer", "minimum": 0 },
        "startUtf16": { "type": "integer", "minimum": 0 },
        "endUtf16": { "type": "integer", "minimum": 0 },
        "startLine": { "type": "integer", "minimum": 0 },
        "endLine": { "type": "integer", "minimum": 0 }
      },
      "additionalProperties": true
    },
    "diagnostic": {
      "type": "object",
      "required": ["severity", "code", "message"],
      "properties": {
        "severity": {
          "type": "string",
          "enum": ["info", "warning", "error"]
        },
        "code": { "type": "string" },
        "message": { "type": "string" },
        "path": { "type": "string" }
      },
      "additionalProperties": true
    }
  },
  "additionalProperties": true
}
```

## Security boundary

raw HTML は default で無効であり，caller が信頼済み source に対して `TrustedHTML` を明示した場合だけ有効にする．custom theme と local asset は project root 内へ解決できるものだけを許可する．Preview HTML 自体は外部 JavaScript を読み込まず，render 中に network access を発生させない．

## 後方互換性

Preview contract は additive API である．既存 `RenderResult` の JSON は root に `html` と `metadata` だけを持ち，Preview の `schemaVersion`，`kind`，`pageMode`，`pages`，`diagnostics` を追加しない．既存 export は公式 Marp CLI を使う経路を維持し，Preview の official Marp Core bridge と lifecycle を共有する必要はない．
