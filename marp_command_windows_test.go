//go:build windows

package renderer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWindowsMarpNpmLaunchersPreserveArguments(t *testing.T) {
	for _, kind := range []string{"local", "global", "javascript", "native"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			bin := filepath.Join(root, "npm tools & 100% 日本語")
			entry := filepath.Join(bin, "node_modules", "@marp-team", "marp-cli", "marp-cli.js")
			launcher := filepath.Join(bin, "marp.cmd")
			if kind == "local" {
				bin = filepath.Join(root, "node_modules", ".bin")
				launcher = filepath.Join(bin, "marp.cmd")
				entry = filepath.Join(root, "node_modules", "@marp-team", "marp-cli", "marp-cli.js")
			}
			copyNativeTool(t, filepath.Join(bin, "node.exe"))
			if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
				t.Fatal(err)
			}
			writeFile(t, entry, "// Native helper replaces Node.js in this test.\n")
			writeFile(t, launcher, "@echo off\nexit /b 99\n")
			writeFile(t, filepath.Join(bin, "marp"), "#!/bin/sh\nexit 99\n")
			t.Setenv("PATH", bin)
			if kind == "local" {
				t.Setenv("PATH", root)
			}
			if kind == "javascript" {
				launcher = entry
			} else if kind == "native" {
				launcher = filepath.Join(bin, "marp.exe")
				copyNativeTool(t, launcher)
			} else if kind == "local" {
				if found := findMarpBinary(root); found != launcher {
					t.Fatalf("found %q, want Windows npm launcher %q", found, launcher)
				}
				launcher = "" // Exercise automatic discovery during conversion too.
			}
			recorded := filepath.Join(root, "args.json")
			t.Setenv("KARTE_RENDERER_TEST_TOOL", "marp")
			t.Setenv("KARTE_TEST_ARGS", recorded)
			input := filepath.Join(root, "slides & %PATH% 日本語.md")
			output := filepath.Join(root, "output & 100% #1.pptx")
			writeFile(t, input, "---\nmarp: true\n---\n# Test")
			if err := ExportMarp(context.Background(), input, output, MarpOptions{Binary: launcher, HTML: true}); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(recorded)
			if err != nil {
				t.Fatal(err)
			}
			var args []string
			if err := json.Unmarshal(data, &args); err != nil {
				t.Fatal(err)
			}
			want := []string{input, "--output", output, "--html"}
			if kind != "native" {
				want = append([]string{entry}, want...)
			}
			if !reflect.DeepEqual(args, want) {
				t.Fatalf("arguments changed: got %#v, want %#v", args, want)
			}
			if result, err := os.ReadFile(output); err != nil || string(result) != "native-marp-output" {
				t.Fatalf("missing Marp output: %q, %v", result, err)
			}
		})
	}
}
