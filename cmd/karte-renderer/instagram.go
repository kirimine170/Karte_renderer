package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/kirimine170/KarteRenderer/instagram"
)

func runInstagram(args []string) error {
	if len(args) == 0 || (args[0] != "preview" && args[0] != "image") {
		return fmt.Errorf("usage: karte-renderer instagram {preview,image} [--root DIR] [--index N] DRAFT.json OUTPUT")
	}
	operation := args[0]
	fs := flag.NewFlagSet("instagram "+operation, flag.ContinueOnError)
	root := fs.String("root", "", "trusted image root (default: draft directory)")
	index := fs.Int("index", 1, "one-based image index for JPEG output")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("expected draft and output paths")
	}
	input, err := filepath.Abs(fs.Arg(0))
	if err != nil {
		return err
	}
	if *root == "" {
		*root = filepath.Dir(input)
	}
	*root, err = filepath.Abs(*root)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(*root, input)
	if err != nil {
		return err
	}
	draft, err := instagram.ReadDraft(*root, filepath.ToSlash(rel))
	if err != nil {
		return err
	}
	output, err := filepath.Abs(fs.Arg(1))
	if err != nil {
		return err
	}
	// Reject aliases before any rendering or truncation．JSON never selects an output path．
	inputs := []string{input}
	for _, media := range draft.Images {
		full, resolveErr := instagram.ResolvePath(*root, media.Filename)
		if resolveErr != nil {
			return resolveErr
		}
		inputs = append(inputs, full)
	}
	outputInfo, _ := os.Stat(output)
	for _, source := range inputs {
		info, _ := os.Stat(source)
		if output == source || (outputInfo != nil && info != nil && os.SameFile(outputInfo, info)) {
			return fmt.Errorf("output must not overwrite the draft or a source image")
		}
	}
	return writeInstagramOutput(output, func(writer io.Writer) error {
		if operation == "image" {
			return instagram.RenderJPEG(context.Background(), *root, draft, *index-1, writer)
		}
		html, err := instagram.PreviewHTML(context.Background(), *root, draft)
		if err != nil {
			return err
		}
		_, err = io.WriteString(writer, html)
		return err
	})
}

func writeInstagramOutput(output string, render func(io.Writer) error) error {
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(output), ".instagram-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := render(f); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), output)
}
