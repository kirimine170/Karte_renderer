package instagram

import (
	"bytes"
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"io/fs"
)

//go:embed web/*
var web embed.FS

// WebAssets is the source of truth for the app and offline CLI preview．
func WebAssets() fs.FS { sub, _ := fs.Sub(web, "web"); return sub }

func PreviewHTML(ctx context.Context, root string, d Draft) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateDraft(d); err != nil {
		return "", err
	}
	if len(d.Images) == 0 {
		return "", fmt.Errorf("a preview requires at least one image")
	}
	media := make([]map[string]any, 0, len(d.Images))
	var pixels int64
	for i, item := range d.Images {
		src, size, err := loadImage(ctx, root, item)
		if err != nil {
			return "", err
		}
		pixels += int64(src.Bounds().Dx()) * int64(src.Bounds().Dy())
		if pixels > 80000000 {
			return "", fmt.Errorf("offline HTML preview exceeds the combined 80 megapixel budget")
		}
		var buf bytes.Buffer
		// Normalize orientation and remove source metadata before embedding．
		if err := jpeg.Encode(&buf, whiteBackground{src}, &jpeg.Options{Quality: 92}); err != nil {
			return "", err
		}
		crop := item.Crop
		if crop == nil {
			crop = &Crop{X: 0.5, Y: 0.5, Zoom: 1}
		}
		media = append(media, map[string]any{"id": i + 1, "name": item.Filename, "url": "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), "width": src.Bounds().Dx(), "height": src.Bounds().Dy(), "type": "image/jpeg", "bytes": size, "crop": crop})
	}
	data, err := json.Marshal(map[string]any{"draft": d, "images": media})
	if err != nil {
		return "", err
	}
	module, _ := fs.ReadFile(WebAssets(), "renderer.mjs")
	css, _ := fs.ReadFile(WebAssets(), "preview.css")
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return `<!doctype html><html lang="ja"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="icon" href="data:,"><title>Instagram draft preview</title><style>
*{box-sizing:border-box}body{margin:0;padding:28px;background:#f6f5f8;color:#584b64;font:13px system-ui,sans-serif}svg{width:19px;height:19px;fill:none;stroke:currentColor;stroke-width:1.65;stroke-linecap:round;stroke-linejoin:round}.svg-library{width:0;height:0;position:absolute;overflow:hidden}h1{font-size:22px}button,select{font:inherit}nav{display:flex;gap:12px;align-items:center;margin:24px 0}nav button{padding:8px 16px;border:1px solid #d6cbe0;background:white;border-radius:6px;color:inherit}#message{color:#91839e;line-height:1.8}#stage{overflow:auto} .device-card-heading{font-size:11px}
` + string(css) + `</style><h1>Instagram 投稿プレビュー</h1><p id="message">暫定ルールによる表示の近似です．実機のフォントや投稿可否は保証しません．画像はこのHTMLに埋め込まれています．</p><nav><button data-view="feed">フィード</button><button data-view="grid">プロフィール</button><label>文字サイズ <select id="scale"><option>100</option><option>125</option><option>150</option></select>%</label></nav><main id="stage" class="device-stage"></main><script id="draft-data" type="application/json">` + string(data) + `</script><script type="module">` + string(module) + `
const payload=JSON.parse(document.querySelector('#draft-data').textContent);
const state={...parseManifest(payload.draft)};
const images=payload.images;
await Promise.all(images.map(async image=>{image.element=new Image();image.element.src=image.url;await image.element.decode();}));
document.body.insertAdjacentHTML('afterbegin',ICONS);
let view='feed',active=0;const expanded=new Set();const renderer=createPostRenderer();
const render=()=>{renderer.configure({state,images,active,view,previewZoom:.72,expanded});renderer.renderComparison(document.querySelector('#stage'));};
const scaleSelect=document.querySelector('#scale');
if(!Array.from(scaleSelect.options).some(option=>option.value===String(state.textScale)))scaleSelect.add(new Option(String(state.textScale),String(state.textScale)));
scaleSelect.value=String(state.textScale);
document.querySelector('#scale').addEventListener('change',event=>{state.textScale=Number(event.target.value);expanded.clear();render();});
document.addEventListener('click',event=>{const tab=event.target.closest('[data-view]');if(tab)view=tab.dataset.view;const slide=event.target.closest('[data-slide]');if(slide)active=(active+Number(slide.dataset.slide)+images.length)%images.length;const image=event.target.closest('[data-image]');if(image)active=Number(image.dataset.image);const more=event.target.closest('[data-expand]');if(more){const id=more.dataset.expand;if(expanded.has(id))expanded.delete(id);else expanded.add(id);}if(tab||slide||image||more)render();});
render();
</script></html>`, nil
}
