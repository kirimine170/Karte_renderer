package renderer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Copies of the test executable act as native tools, so these command tests
// exercise Windows without depending on POSIX shell scripts or installed tools.
func TestMain(m *testing.M) {
	if mode := os.Getenv("KARTE_RENDERER_TEST_TOOL"); mode != "" {
		if err := runNativeToolHelper(mode); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func runNativeToolHelper(mode string) error {
	if mode == "probe" {
		args, _ := json.Marshal(os.Args[1:])
		if err := os.WriteFile(os.Getenv("KARTE_TEST_ARGS"), args, 0o600); err != nil {
			return err
		}
		fmt.Println(`<meta name="karte-preflight-overflow" content="%5B%5D">`)
		return nil
	}
	if mode == "child" {
		for {
			if err := os.WriteFile(os.Getenv("KARTE_TEST_HEARTBEAT"), []byte(strconv.FormatInt(time.Now().UnixNano(), 10)), 0o600); err != nil {
				return err
			}
			time.Sleep(25 * time.Millisecond)
		}
	}
	if mode == "marp" {
		args, _ := json.Marshal(os.Args[1:])
		if err := os.WriteFile(os.Getenv("KARTE_TEST_ARGS"), args, 0o600); err != nil {
			return err
		}
		for i, arg := range os.Args[1:] {
			if arg == "--output" && i+2 < len(os.Args) {
				return os.WriteFile(os.Args[i+2], []byte("native-marp-output"), 0o600)
			}
		}
		return fmt.Errorf("missing Marp output argument")
	}
	child := exec.Command(os.Args[0])
	child.Env = append(os.Environ(), "KARTE_RENDERER_TEST_TOOL=child")
	child.Stdout, child.Stderr = os.Stdout, os.Stderr
	if err := child.Start(); err != nil {
		return err
	}
	if err := os.WriteFile(os.Getenv("KARTE_TEST_CHILD_PID"), []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
		return err
	}
	deadline := time.Now().Add(5 * time.Second)
	for !regularFileExists(os.Getenv("KARTE_TEST_HEARTBEAT")) {
		if time.Now().After(deadline) {
			return fmt.Errorf("child did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if mode == "pdf" {
		for _, arg := range os.Args[1:] {
			if output, ok := strings.CutPrefix(arg, "--print-to-pdf="); ok {
				if err := os.WriteFile(output, []byte("%PDF-1.4\n1 0 obj\n<<>>\nendobj\n%%EOF\n"), 0o600); err != nil {
					return err
				}
			}
		}
	}
	time.Sleep(time.Minute)
	return nil
}

func copyNativeTool(t *testing.T, destination string) {
	t.Helper()
	data, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, data, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestNativeOverflowProbeUsesFileURL(t *testing.T) {
	root := filepath.Join(t.TempDir(), "日本語 #1 100%")
	binary := filepath.Join(root, "fake-chromium")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	copyNativeTool(t, binary)
	recorded := filepath.Join(root, "args.json")
	t.Setenv("KARTE_RENDERER_TEST_TOOL", "probe")
	t.Setenv("KARTE_TEST_ARGS", recorded)
	input := filepath.Join(root, "page.html")
	writeFile(t, input, "<!doctype html><p>test</p>")
	result := checkHTMLOverflow(context.Background(), input, PreflightOptions{ChromiumBinary: binary})
	if !result.Passed {
		t.Fatalf("native overflow probe failed: %+v", result)
	}
	data, err := os.ReadFile(recorded)
	if err != nil {
		t.Fatal(err)
	}
	var args []string
	if err := json.Unmarshal(data, &args); err != nil || len(args) == 0 {
		t.Fatalf("missing browser arguments: %s, %v", data, err)
	}
	inputURL := args[len(args)-1]
	parsed, err := url.Parse(inputURL)
	if err != nil || parsed.Scheme != "file" || parsed.Host != "" || parsed.Opaque != "" || parsed.Fragment != "" {
		t.Fatalf("invalid browser input URL: %s, %v", inputURL, err)
	}
	if filepath.Dir(filePathFromURL(parsed, runtime.GOOS)) != root || strings.Contains(inputURL, "%5C") {
		t.Fatalf("browser input URL changed the path: %s", inputURL)
	}
}

func TestNativeChromiumStopsDescendants(t *testing.T) {
	for _, mode := range []string{"pdf", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			binary := filepath.Join(root, "fake-chromium")
			if runtime.GOOS == "windows" {
				binary += ".exe"
			}
			copyNativeTool(t, binary)
			heartbeat := filepath.Join(root, "heartbeat")
			pidPath := filepath.Join(root, "child.pid")
			t.Setenv("KARTE_RENDERER_TEST_TOOL", mode)
			t.Setenv("KARTE_TEST_HEARTBEAT", heartbeat)
			t.Setenv("KARTE_TEST_CHILD_PID", pidPath)
			t.Cleanup(func() {
				if data, err := os.ReadFile(pidPath); err == nil {
					if pid, err := strconv.Atoi(string(data)); err == nil {
						if process, err := os.FindProcess(pid); err == nil {
							_ = process.Kill()
							_ = process.Release()
						}
					}
				}
			})
			input := filepath.Join(root, "日本語 #1.html")
			writeFile(t, input, "<!doctype html><title>native tool</title>")
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			err := ExportHTMLPDF(ctx, input, filepath.Join(root, "output.pdf"), PDFOptions{Engine: "chromium", Binary: binary})
			if mode == "pdf" && err != nil {
				t.Fatal(err)
			}
			if mode == "cancel" && (err == nil || !strings.Contains(err.Error(), "deadline exceeded")) {
				t.Fatalf("expected cancellation error, got %v", err)
			}
			before, err := os.ReadFile(heartbeat)
			if err != nil {
				t.Fatalf("browser child did not run: %v", err)
			}
			time.Sleep(150 * time.Millisecond)
			after, err := os.ReadFile(heartbeat)
			if err != nil || string(before) != string(after) {
				t.Fatalf("browser child survived %s: %v", mode, err)
			}
		})
	}
}
