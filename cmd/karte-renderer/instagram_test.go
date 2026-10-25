package main

import (
	"encoding/json"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kirimine170/KarteRenderer/instagram"
)

func TestInstagramCLI(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "draft.json")
	source := filepath.Join(root, "source.png")
	f, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, 160, 90))); err != nil {
		t.Fatal(err)
	}
	f.Close()
	d := instagram.Draft{Schema: "karte.instagram-draft", Version: 1, Mode: "app", Aspect: "0.8", Images: []instagram.Media{{Filename: "source.png"}}}
	b, _ := json.Marshal(d)
	if err := os.WriteFile(input, b, 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "out.jpg")
	if err := run([]string{"instagram", "image", "--root", root, input, output}); err != nil {
		t.Fatal(err)
	}
	f, err = os.Open(output)
	if err != nil {
		t.Fatal(err)
	}
	config, err := jpeg.DecodeConfig(f)
	f.Close()
	if err != nil || config.Width != 1080 || config.Height != 1350 {
		t.Fatalf("invalid output: %+v %v", config, err)
	}
	preview := filepath.Join(root, "preview.html")
	if err := run([]string{"instagram", "preview", input, preview}); err != nil {
		t.Fatal(err)
	}
	html, _ := os.ReadFile(preview)
	if !strings.Contains(string(html), "createPostRenderer") || !strings.Contains(string(html), "data:image/jpeg;base64,") {
		t.Fatal("preview is not self contained")
	}
	before, _ := os.ReadFile(source)
	if err := run([]string{"instagram", "image", input, source}); err == nil {
		t.Fatal("overwrote source")
	}
	after, _ := os.ReadFile(source)
	if string(before) != string(after) {
		t.Fatal("source changed")
	}
	if err := run([]string{"instagram", "image", "--index", "0", input, output}); err == nil {
		t.Fatal("accepted invalid index")
	}
	f, err = os.Open(output)
	if err != nil {
		t.Fatal(err)
	}
	_, err = jpeg.DecodeConfig(f)
	f.Close()
	if err != nil {
		t.Fatal("failed rendering replaced an existing output")
	}
	if err := run([]string{"instagram", "unknown", input, output}); err == nil {
		t.Fatal("accepted unknown operation")
	}
}
