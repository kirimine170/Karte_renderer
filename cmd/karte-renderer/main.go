package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	renderer "github.com/kirimine170/KarteRenderer"
)

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

type optionalBool struct{ target **bool }

func (b *optionalBool) String() string {
	if b == nil || b.target == nil || *b.target == nil {
		return ""
	}
	return strconv.FormatBool(**b.target)
}

func (b *optionalBool) Set(value string) error {
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return err
	}
	*b.target = &parsed
	return nil
}

func (b *optionalBool) IsBoolFlag() bool { return true }

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "karte-renderer:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 && args[0] == "doctor" {
		return doctor()
	}
	if len(args) > 0 && args[0] == "convert" {
		args = args[1:]
	}
	fs := flag.NewFlagSet("karte-renderer", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var opts renderer.ConvertOptions
	var themeSets stringList
	fs.StringVar(&opts.Root, "root", "", "trusted project root (default: input directory)")
	fs.BoolVar(&opts.HardWrap, "hardwrap", false, "render Markdown soft line breaks as <br>")
	fs.StringVar(&opts.CSS, "css", "", "replace the default document CSS with the specified file")
	fs.BoolVar(&opts.NoCSS, "no-css", false, "disable CSS for normal Markdown documents")
	fs.StringVar(&opts.Marp.Binary, "marp-binary", "", "path to the Marp CLI")
	fs.StringVar(&opts.Marp.Theme, "theme", "", "Marp theme name or CSS path")
	fs.Var(&themeSets, "theme-set", "additional Marp theme CSS (repeatable)")
	fs.BoolVar(&opts.Marp.HTML, "html", false, "allow trusted HTML in Marp Markdown")
	fs.BoolVar(&opts.Marp.AllowLocalFiles, "allow-local-files", false, "allow Marp to load trusted local assets")
	fs.StringVar(&opts.Marp.BrowserPath, "browser-path", "", "browser executable used by Marp")
	fs.BoolVar(&opts.Marp.EditablePPTX, "pptx-editable", false, "request experimental editable PPTX output")
	fs.StringVar(&opts.PDF.Engine, "pdf-engine", "auto", "document PDF engine: auto, chromium, or wkhtmltopdf")
	fs.StringVar(&opts.PDF.Binary, "pdf-binary", "", "path to the document PDF engine")
	fs.StringVar(&opts.PDF.PageSize, "pdf-page-size", "", "PDF page size: A3, A4, A5, B4, B5, Letter, or Legal")
	fs.StringVar(&opts.PDF.Orientation, "pdf-orientation", "", "PDF page orientation: portrait or landscape")
	fs.StringVar(&opts.PDF.Margin, "pdf-margin", "", "PDF page margin (one to four physical lengths)")
	fs.StringVar(&opts.PDF.InsideMargin, "pdf-inside-margin", "", "PDF binding-edge margin")
	fs.StringVar(&opts.PDF.OutsideMargin, "pdf-outside-margin", "", "PDF outside-edge margin")
	fs.StringVar(&opts.PDF.Header, "pdf-header", "", "PDF running header text")
	fs.StringVar(&opts.PDF.Footer, "pdf-footer", "", "PDF running footer text")
	fs.Var(&optionalBool{target: &opts.PDF.PageNumbers}, "pdf-page-numbers", "show PDF page-number folios (may be set to false)")
	fs.StringVar(&opts.PDF.ChapterStart, "pdf-chapter-start", "", "chapter page side: right or any")
	fs.BoolVar(&opts.Preflight.Enabled, "preflight", false, "validate PDF pages, size, fonts, assets, TeX, and overflow")
	fs.IntVar(&opts.Preflight.ExpectedPages, "expected-pages", 0, "required PDF page count (also enables preflight)")
	fs.StringVar(&opts.Preflight.ReportPath, "preflight-report", "", "write preflight JSON report to this path")
	fs.StringVar(&opts.Preflight.PDFInfoBinary, "pdfinfo-binary", "", "path to Poppler pdfinfo")
	fs.StringVar(&opts.Preflight.PDFFontsBinary, "pdffonts-binary", "", "path to Poppler pdffonts")
	fs.StringVar(&opts.Preflight.PDFToTextBinary, "pdftotext-binary", "", "path to Poppler pdftotext")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: karte-renderer [convert] [options] INPUT.md OUTPUT.{html,pdf,pptx}")
		fmt.Fprintln(fs.Output(), "       karte-renderer doctor")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		fs.Usage()
		return fmt.Errorf("expected input and output paths")
	}
	opts.Marp.ThemeSet = themeSets
	_, err := renderer.ConvertFile(context.Background(), fs.Arg(0), fs.Arg(1), opts)
	return err
}

func doctor() error {
	cwd, _ := os.Getwd()
	for _, dep := range renderer.Diagnose(cwd) {
		status := "missing"
		if dep.Found {
			status = dep.Path
		}
		fmt.Printf("%-14s %-7s %s\n", dep.Name, status, dep.RequiredFor)
	}
	return nil
}
