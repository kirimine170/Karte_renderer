// Package instagram renders local Instagram drafts without application or Wails dependencies．
package instagram

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
)

const MaxDraftBytes = 1 << 20

type Crop struct {
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Zoom float64 `json:"zoom"`
}

type Media struct {
	Filename string `json:"filename"`
	Crop     *Crop  `json:"crop,omitempty"`
}

type Simulation struct {
	Devices        []string `json:"devices,omitempty"`
	TextScale      float64  `json:"textScale,omitempty"`
	CollapsedLines int      `json:"collapsedLines,omitempty"`
	GridRatio      float64  `json:"gridRatio,omitempty"`
	CustomWidth    int      `json:"customWidth,omitempty"`
	CustomHeight   int      `json:"customHeight,omitempty"`
}

type Draft struct {
	Schema     string     `json:"schema"`
	Version    int        `json:"version"`
	Mode       string     `json:"mode"`
	Username   string     `json:"username"`
	Location   string     `json:"location"`
	Caption    string     `json:"caption"`
	Aspect     string     `json:"aspect"`
	Simulation Simulation `json:"simulation"`
	Images     []Media    `json:"images"`
}

func ParseDraft(data []byte) (Draft, error) {
	var d Draft
	if len(data) > MaxDraftBytes {
		return d, fmt.Errorf("draft exceeds 1 MiB")
	}
	if err := json.Unmarshal(data, &d); err != nil {
		return d, fmt.Errorf("decode Instagram draft: %w", err)
	}
	if d.Schema != "karte.instagram-draft" || d.Version != 1 {
		return d, fmt.Errorf("unsupported Instagram draft schema or version")
	}
	if d.Mode != "app" && d.Mode != "api" {
		return d, fmt.Errorf("mode must be app or api")
	}
	if len(d.Images) > 20 {
		return d, fmt.Errorf("a draft supports at most 20 images")
	}
	if len(d.Caption) > 200000 {
		return d, fmt.Errorf("caption is too large")
	}
	switch d.Aspect {
	case "auto", "0.75", "0.8", "1", "1.91":
	default:
		return d, fmt.Errorf("unsupported aspect %q", d.Aspect)
	}
	for i, media := range d.Images {
		if media.Filename == "" {
			return d, fmt.Errorf("image %d has no filename", i+1)
		}
		if media.Crop != nil {
			c := media.Crop
			if !finite(c.X) || !finite(c.Y) || !finite(c.Zoom) || c.X < 0 || c.X > 1 || c.Y < 0 || c.Y > 1 || c.Zoom < 1 || c.Zoom > 3 {
				return d, fmt.Errorf("image %d has invalid crop coordinates", i+1)
			}
		}
	}
	return d, nil
}

func finite(n float64) bool { return !math.IsNaN(n) && !math.IsInf(n, 0) }

func validateDraft(d Draft) error {
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	_, err = ParseDraft(b)
	return err
}

// ResolvePath confines local input files to root，including symlink targets．
// Filenames in a draft are root-relative paths，never URLs or absolute paths．
func ResolvePath(root, name string) (string, error) {
	if filepath.IsAbs(name) || strings.Contains(name, "\\") || strings.Contains(name, ":") {
		return "", fmt.Errorf("expected a root-relative local path: %q", name)
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	full, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("input escapes project root: %q", name)
	}
	return full, nil
}

func ReadDraft(root, relativePath string) (Draft, error) {
	full, err := ResolvePath(root, relativePath)
	if err != nil {
		return Draft{}, err
	}
	f, err := os.Open(full)
	if err != nil {
		return Draft{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return Draft{}, err
	}
	if !info.Mode().IsRegular() {
		return Draft{}, fmt.Errorf("draft must be a regular file")
	}
	b, err := io.ReadAll(io.LimitReader(f, MaxDraftBytes+1))
	if err != nil {
		return Draft{}, err
	}
	return ParseDraft(b)
}

type Rect struct{ SX, SY, Width, Height float64 }

// CropRect uses the same fractional source coordinates as the browser renderer．
func CropRect(width, height int, ratio float64, crop *Crop) (Rect, error) {
	if width <= 0 || height <= 0 || !finite(ratio) || ratio <= 0 {
		return Rect{}, fmt.Errorf("invalid crop dimensions")
	}
	c := Crop{X: 0.5, Y: 0.5, Zoom: 1}
	if crop != nil {
		c = *crop
	}
	if !finite(c.X) || !finite(c.Y) || !finite(c.Zoom) || c.X < 0 || c.X > 1 || c.Y < 0 || c.Y > 1 || c.Zoom < 1 || c.Zoom > 3 {
		return Rect{}, fmt.Errorf("invalid crop coordinates")
	}
	sw := math.Min(float64(width), float64(height)*ratio) / c.Zoom
	sh := sw / ratio
	return Rect{(float64(width) - sw) * c.X, (float64(height) - sh) * c.Y, sw, sh}, nil
}

func (d Draft) Ratio(firstWidth, firstHeight int) float64 {
	r := 0.8
	switch d.Aspect {
	case "auto":
		if firstHeight > 0 {
			r = float64(firstWidth) / float64(firstHeight)
		}
	case "0.75":
		r = 0.75
	case "1":
		r = 1
	case "1.91":
		r = 1.91
	}
	minimum := 0.75
	if d.Mode == "api" {
		minimum = 0.8
	}
	return math.Max(minimum, math.Min(1.91, r))
}
