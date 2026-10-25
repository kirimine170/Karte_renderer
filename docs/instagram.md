# Instagram画像投稿のレンダリング

`instagram`パッケージはKarteの編集画面から独立しています．下書きJSON，投稿比率，切り出し計算，フィード・プロフィールの表示，JPEG書き出しを担当します．編集操作，下書きの自動保存，ファイル選択・保存ダイアログは呼び出し元の責務です．

## CLI

```sh
go build -o bin/karte-renderer ./cmd/karte-renderer

# 元画像を埋め込んだ，オフラインで開けるHTMLプレビュー
bin/karte-renderer instagram preview examples/instagram/draft.json output/instagram.html

# 幅1080pxのJPEG．--indexは1始まり，既定は1枚目
bin/karte-renderer instagram image --index 1 examples/instagram/draft.json output/instagram.jpg

# 画像ルートを明示する場合
bin/karte-renderer instagram image --root /path/to/project /path/to/project/drafts/post.json output/post.jpg
```

既存のMarkdown・Marp変換と`doctor`のCLIはそのまま使えます．Instagram用のコマンドは別のサブコマンドです．JPEG出力もHTML生成もNode.js，Chromium，Wailsを必要としません．生成されたHTMLを見るときは通常のブラウザを使います．HTML内のスクリプトが画面幅ごとの文字の折り返しを計測します．CLIからInstagramへの投稿は行いません．

`--root`省略時はJSONの置かれたディレクトリがルートです．`images[].filename`はそのルートに対する相対パスです．JSON自身もルート内に置きます．絶対パス，URL，ルート外への相対パス・シンボリックリンクは受け付けません．出力先は利用者がCLI引数で指定し，元画像や下書きへの上書きは拒否します．失敗時は既存の出力ファイルを維持します．

## JSON契約

```json
{
  "schema": "karte.instagram-draft",
  "version": 1,
  "mode": "app",
  "username": "karte_studio",
  "location": "Kyoto, Japan",
  "caption": "小さな発見を，日々のなかに．",
  "aspect": "0.8",
  "simulation": {
    "devices": ["compact", "standard", "android"],
    "textScale": 100,
    "collapsedLines": 2,
    "gridRatio": 0.75
  },
  "images": [
    { "filename": "source.jpg", "crop": { "x": 0.5, "y": 0.5, "zoom": 1 } }
  ]
}
```

`aspect`は`auto`，`0.75`，`0.8`，`1`，`1.91`です．`auto`は先頭の元画像から比率を決め，投稿経路の想定範囲に収めます．切り出しの`x`・`y`は移動可能な範囲の0〜1で，`zoom`は1〜3です．`crop`省略時は中央・等倍です．全画像に共通比率を使い，プロフィールではその投稿画像に対して中央トリミングを追加します．

ブラウザが追加する`width`，`height`，`mimeType`，`bytes`，`sourceRect`は情報用です．Go側は実ファイルの寸法とEXIFの向きを読み，信頼する座標を再計算します．EXIFの向き補正後の画素を座標系に使います．画像本体はJSONに含めません．Karteで保存したJSONをCLIで使う場合は，同じファイル名の元画像をルート配下に配置してください．

読み込みはJPEG・PNG・WebPに対応します．JPEG出力の透明領域は白です．JSONは1MiB，画像は1枚30MiB・8000万画素，画像数は20枚までです．HTMLの埋め込みは画像全体で8000万画素までです．これらはツールの実装上限で，Instagramの受理仕様ではありません．

## Go APIとWebモジュール

```go
draft, err := instagram.ReadDraft(projectRoot, "draft.json")
// エラー処理後，呼び出し側が選択したWriterへ出力
err = instagram.RenderJPEG(ctx, projectRoot, draft, 0, writer)
html, err := instagram.PreviewHTML(ctx, projectRoot, draft)
```

`WebAssets()`は`renderer.mjs`と`preview.css`を返す読み取り専用の埋め込みFSです．Karteでは固定したGo依存からこれらを生成・配布し，描画処理をアプリ側で別実装しません．`createPostRenderer()`の`configure`に状態と読み込み済み画像を渡し，`renderComparison`・`drawPost`・`buildPostCanvas`を呼びます．ファイル保存とイベント処理は含みません．

GoとWebでは切り出しの比率・座標を共通の契約で扱います．画像の再サンプリングとJPEG圧縮はそれぞれの実装を使うため，出力バイトや画素の厳密一致は保証しません．フォント・OS描画・HDR・Instagramの再圧縮も再現対象外です．

## 暫定の投稿ルール

アプリは3:4〜1.91:1・20枚，APIは4:5〜1.91:1・10枚，キャプションは2200文字を想定します．ハッシュタグは5個超で注意を表示します．2026年10月25日の調査ではMetaの公式資料にアクセス制限があり，最新値の本文確認はできていません．アカウント・APIバージョンごとの実際の制約を別途確認してください．これらは投稿可能であることを保証する判定ではありません．

公式の再確認先は[写真の解像度](https://help.instagram.com/1631821640426723)と[Content Publishing](https://developers.facebook.com/docs/instagram-platform/content-publishing/)です．
