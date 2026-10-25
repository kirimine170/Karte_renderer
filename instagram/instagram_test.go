package instagram

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testDraft() Draft {
	return Draft{Schema: "karte.instagram-draft", Version: 1, Mode: "app", Aspect: "0.8", Caption: "test", Images: []Media{{Filename: "source.png"}}}
}

func writeSource(t *testing.T, root string) {
	t.Helper()
	src := image.NewNRGBA(image.Rect(0, 0, 160, 90))
	for y := 0; y < 90; y++ {
		for x := 0; x < 160; x++ {
			if x < 80 {
				src.Set(x, y, color.NRGBA{R: 255, A: 255})
			} else {
				src.Set(x, y, color.NRGBA{B: 255, A: 255})
			}
		}
	}
	f, err := os.Create(filepath.Join(root, "source.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err = png.Encode(f, src); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCropGeometry(t *testing.T) {
	c, err := CropRect(1080, 1080, .75, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.SX != 135 || c.SY != 0 || c.Width != 810 || c.Height != 1080 {
		t.Fatalf("unexpected crop: %+v", c)
	}
	for _, ratio := range []float64{.75, .8, 1, 1.91} {
		for _, zoom := range []float64{1, 1.75, 3} {
			for _, x := range []float64{0, .5, 1} {
				c, err := CropRect(1600, 900, ratio, &Crop{X: x, Y: 1, Zoom: zoom})
				if err != nil {
					t.Fatal(err)
				}
				if c.SX < 0 || c.SY < 0 || c.SX+c.Width > 1600+1e-8 || c.SY+c.Height > 900+1e-8 || math.Abs(c.Width/c.Height-ratio) > 1e-8 {
					t.Fatalf("out of bounds: %+v", c)
				}
			}
		}
	}
	if _, err := CropRect(1, 1, math.NaN(), nil); err == nil {
		t.Fatal("accepted NaN")
	}
}

func TestDraftContract(t *testing.T) {
	d := testDraft()
	d.Images[0].Crop = &Crop{X: 0, Y: 1, Zoom: 1.75}
	b, _ := json.Marshal(d)
	parsed, err := ParseDraft(b)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Images[0].Crop.X != 0 || parsed.Images[0].Crop.Zoom != 1.75 {
		t.Fatal("crop changed")
	}
	d.Version = 99
	b, _ = json.Marshal(d)
	if _, err := ParseDraft(b); err == nil {
		t.Fatal("accepted future schema")
	}
	d = testDraft()
	d.Images[0].Crop = &Crop{X: 2, Y: 0, Zoom: 1}
	b, _ = json.Marshal(d)
	if _, err := ParseDraft(b); err == nil {
		t.Fatal("accepted invalid crop")
	}
	d = testDraft()
	d.Aspect = "auto"
	if d.Ratio(1080, 1920) != .75 {
		t.Fatal("wrong app ratio")
	}
	d.Mode = "api"
	if d.Ratio(1080, 1920) != .8 {
		t.Fatal("wrong API ratio")
	}
}

func TestRenderJPEGAndTransparentBackground(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root)
	d := testDraft()
	var b bytes.Buffer
	if err := RenderJPEG(context.Background(), root, d, 0, &b); err != nil {
		t.Fatal(err)
	}
	src, err := jpeg.Decode(bytes.NewReader(b.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if src.Bounds().Dx() != 1080 || src.Bounds().Dy() != 1350 {
		t.Fatal(src.Bounds())
	}
	r, _, blue, _ := src.At(30, 500).RGBA()
	if r < 50000 || blue > 10000 {
		t.Fatal("left source color lost")
	}
	_, _, blue, _ = src.At(1050, 500).RGBA()
	if blue < 50000 {
		t.Fatal("right source color lost")
	}
	f, _ := os.Create(filepath.Join(root, "transparent.png"))
	if err := png.Encode(f, image.NewNRGBA(image.Rect(0, 0, 50, 50))); err != nil {
		t.Fatal(err)
	}
	f.Close()
	d.Images[0].Filename = "transparent.png"
	b.Reset()
	if err := RenderJPEG(context.Background(), root, d, 0, &b); err != nil {
		t.Fatal(err)
	}
	src, _ = jpeg.Decode(&b)
	r, g, blue, _ := src.At(500, 500).RGBA()
	if r < 65000 || g < 65000 || blue < 65000 {
		t.Fatal("transparent area is not white")
	}
}

func TestPreviewEscapingAndCancellation(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root)
	d := testDraft()
	d.Caption = "</script><script>alert('x')</script>"
	html, err := PreviewHTML(context.Background(), root, d)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, d.Caption) || !strings.Contains(html, "data:image/jpeg;base64,") || !strings.Contains(html, "createPostRenderer") {
		t.Fatal("unsafe or incomplete preview")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var b bytes.Buffer
	if err := RenderJPEG(ctx, root, d, 0, &b); err == nil || b.Len() != 0 {
		t.Fatal("canceled rendering wrote output")
	}
}

func TestPathConfinement(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root)
	for _, name := range []string{"../outside.png", "/tmp/image.png", "https://example.com/a.jpg", "..\\outside.png"} {
		if _, err := ResolvePath(root, name); err == nil {
			t.Fatalf("accepted %s", name)
		}
	}
	outside := filepath.Join(t.TempDir(), "outside.png")
	os.WriteFile(outside, []byte("test"), 0600)
	if err := os.Symlink(outside, filepath.Join(root, "escape.png")); err == nil {
		if _, err := ResolvePath(root, "escape.png"); err == nil {
			t.Fatal("followed external symlink")
		}
	}
	if _, err := ParseDraft(make([]byte, MaxDraftBytes+1)); err == nil {
		t.Fatal("accepted oversized draft")
	}
}

func TestEXIFOrientation(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	src.Set(0, 0, color.NRGBA{R: 255, A: 255})
	for orientation := 1; orientation <= 8; orientation++ {
		segment := make([]byte, 32)
		copy(segment, "Exif\x00\x00II")
		binary.LittleEndian.PutUint16(segment[8:10], 42)
		binary.LittleEndian.PutUint32(segment[10:14], 8)
		binary.LittleEndian.PutUint16(segment[14:16], 1)
		binary.LittleEndian.PutUint16(segment[16:18], 0x112)
		binary.LittleEndian.PutUint16(segment[18:20], 3)
		binary.LittleEndian.PutUint32(segment[20:24], 1)
		binary.LittleEndian.PutUint16(segment[24:26], uint16(orientation))
		b := []byte{0xff, 0xd8, 0xff, 0xe1, 0, byte(len(segment) + 2)}
		b = append(b, segment...)
		if exifOrientation(b) != orientation {
			t.Fatalf("orientation %d not read", orientation)
		}
		oriented := orientImage(src, orientation)
		if orientation >= 5 && (oriented.Bounds().Dx() != 2 || oriented.Bounds().Dy() != 3) {
			t.Fatal("rotation did not swap dimensions")
		}
		expected := [][2]int{{0, 0}, {2, 0}, {2, 1}, {0, 1}, {0, 0}, {1, 0}, {1, 2}, {0, 2}}[orientation-1]
		r, _, _, _ := oriented.At(expected[0], expected[1]).RGBA()
		if r != 65535 {
			t.Fatalf("orientation %d misplaced source pixel", orientation)
		}
	}
	for i := 0; i < 100; i++ {
		exifOrientation(bytes.Repeat([]byte{0xff, 0xe1}, i))
	}
}
