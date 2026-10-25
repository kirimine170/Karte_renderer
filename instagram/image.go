package instagram

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"io"
	"math"
	"os"

	"golang.org/x/image/draw"
	"golang.org/x/image/math/f64"
	_ "golang.org/x/image/webp"
)

const MaxImageBytes = 30 << 20
const MaxImagePixels = 80000000

func loadImage(ctx context.Context, root string, media Media) (image.Image, int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	full, err := ResolvePath(root, media.Filename)
	if err != nil {
		return nil, 0, fmt.Errorf("resolve image %q: %w", media.Filename, err)
	}
	f, err := os.Open(full)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxImageBytes {
		return nil, 0, fmt.Errorf("image must be a regular file of at most 30 MiB")
	}
	b, err := io.ReadAll(io.LimitReader(f, MaxImageBytes+1))
	if err != nil {
		return nil, 0, err
	}
	if len(b) > MaxImageBytes {
		return nil, 0, fmt.Errorf("image exceeds 30 MiB")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return nil, 0, fmt.Errorf("read image header: %w", err)
	}
	if format != "jpeg" && format != "png" && format != "webp" {
		return nil, 0, fmt.Errorf("unsupported image format %q", format)
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > MaxImagePixels {
		return nil, 0, fmt.Errorf("image exceeds 80 megapixels")
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	src, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, 0, fmt.Errorf("decode image: %w", err)
	}
	if format == "jpeg" {
		src = orientImage(src, exifOrientation(b))
	}
	return src, int64(len(b)), ctx.Err()
}

// RenderJPEG writes a 1080px-wide post image．It needs neither Node，a browser，nor Wails．
// index is zero-based．The first source determines the common ratio in auto mode．
func RenderJPEG(ctx context.Context, root string, d Draft, index int, output io.Writer) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateDraft(d); err != nil {
		return err
	}
	if index < 0 || index >= len(d.Images) {
		return fmt.Errorf("image index is outside the draft")
	}
	first, _, err := loadImage(ctx, root, d.Images[0])
	if err != nil {
		return err
	}
	ratio := d.Ratio(first.Bounds().Dx(), first.Bounds().Dy())
	src := first
	if index != 0 {
		src, _, err = loadImage(ctx, root, d.Images[index])
		if err != nil {
			return err
		}
	}
	c, err := CropRect(src.Bounds().Dx(), src.Bounds().Dy(), ratio, d.Images[index].Crop)
	if err != nil {
		return err
	}
	dst := image.NewRGBA(image.Rect(0, 0, 1080, int(math.Floor(1080/ratio+0.5))))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	sx, sy := 1080/c.Width, float64(dst.Bounds().Dy())/c.Height
	draw.CatmullRom.Transform(dst, f64.Aff3{sx, 0, -c.SX * sx, 0, sy, -c.SY * sy}, src, src.Bounds(), draw.Over, nil)
	if err := ctx.Err(); err != nil {
		return err
	}
	return jpeg.Encode(output, dst, &jpeg.Options{Quality: 92})
}

type whiteBackground struct{ image.Image }

func (w whiteBackground) At(x, y int) color.Color {
	r, g, b, a := w.Image.At(x, y).RGBA()
	return color.RGBA{uint8((r + 65535 - a) >> 8), uint8((g + 65535 - a) >> 8), uint8((b + 65535 - a) >> 8), 255}
}

// EXIF applies to source coordinates before cropping．Malformed metadata is ignored．
func exifOrientation(b []byte) int {
	if len(b) < 4 || b[0] != 0xff || b[1] != 0xd8 {
		return 1
	}
	for p := 2; p+4 <= len(b); {
		if b[p] != 0xff {
			break
		}
		marker := b[p+1]
		if marker == 0xda || marker == 0xd9 {
			break
		}
		if marker == 0xff {
			p++
			continue
		}
		n := int(binary.BigEndian.Uint16(b[p+2 : p+4]))
		if n < 2 || n > len(b)-p-2 {
			break
		}
		segment := b[p+4 : p+2+n]
		if marker == 0xe1 && len(segment) >= 14 && string(segment[:6]) == "Exif\x00\x00" {
			t := segment[6:]
			var order binary.ByteOrder
			switch string(t[:2]) {
			case "II":
				order = binary.LittleEndian
			case "MM":
				order = binary.BigEndian
			default:
				return 1
			}
			if order.Uint16(t[2:4]) != 42 {
				return 1
			}
			offset := uint64(order.Uint32(t[4:8]))
			if offset+2 > uint64(len(t)) {
				return 1
			}
			count := int(order.Uint16(t[offset : offset+2]))
			for j := 0; j < count; j++ {
				start := offset + 2 + uint64(j)*12
				if start+12 > uint64(len(t)) {
					return 1
				}
				e := t[start : start+12]
				if order.Uint16(e[:2]) == 0x112 && order.Uint16(e[2:4]) == 3 && order.Uint32(e[4:8]) == 1 {
					v := int(order.Uint16(e[8:10]))
					if v >= 1 && v <= 8 {
						return v
					}
					return 1
				}
			}
		}
		p += 2 + n
	}
	return 1
}

func orientImage(src image.Image, orientation int) image.Image {
	if orientation <= 1 || orientation > 8 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dw, dh := w, h
	if orientation >= 5 {
		dw, dh = h, w
	}
	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx, dy := x, y
			switch orientation {
			case 2:
				dx = w - 1 - x
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dy = h - 1 - y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			dst.Set(dx, dy, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}
