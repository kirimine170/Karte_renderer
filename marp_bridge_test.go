package renderer

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

const marpBridgeHelperEnv = "GO_WANT_MARP_BRIDGE_HELPER"

func TestMarpBridgeHelperProcess(t *testing.T) {
	if os.Getenv(marpBridgeHelperEnv) != "1" {
		return
	}
	var outputMu sync.Mutex
	write := func(response marpWireResponse) {
		outputMu.Lock()
		defer outputMu.Unlock()
		payload, _ := json.Marshal(response)
		fmt.Println(string(payload))
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request marpWireRequest
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			os.Exit(2)
		}
		if request.ID == "" {
			os.Exit(2)
		}
		switch request.Source {
		case "crash":
			fmt.Fprintln(os.Stderr, "intentional helper crash")
			os.Exit(3)
		case "slow":
			go func(id string) {
				time.Sleep(150 * time.Millisecond)
				write(marpWireResponse{ID: id, Result: MarpRenderResponse{HTML: "late"}})
			}(request.ID)
		default:
			write(marpWireResponse{ID: request.ID, Result: MarpRenderResponse{
				HTML:     request.Source,
				CSS:      "section{}",
				Comments: [][]string{{"note"}},
			}})
		}
	}
	os.Exit(0)
}

func newFakeMarpBridge(t *testing.T) *MarpBridge {
	t.Helper()
	helper := t.TempDir() + "/helper.mjs"
	if err := os.WriteFile(helper, []byte("test helper placeholder"), 0o600); err != nil {
		t.Fatal(err)
	}
	bridge := NewMarpBridge(MarpBridgeOptions{NodePath: "test-node", HelperPath: helper})
	bridge.command = func(_ string, _ ...string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=^TestMarpBridgeHelperProcess$")
		cmd.Env = append(os.Environ(), marpBridgeHelperEnv+"=1")
		return cmd
	}
	t.Cleanup(func() { _ = bridge.Close() })
	return bridge
}

func TestMarpBridgeReusesProcessAndMapsResponses(t *testing.T) {
	bridge := newFakeMarpBridge(t)
	for _, source := range []string{"first", "second"} {
		response, err := bridge.Render(context.Background(), MarpRenderRequest{Root: t.TempDir(), Source: source})
		if err != nil {
			t.Fatal(err)
		}
		if response.HTML != source || response.CSS != "section{}" {
			t.Fatalf("unexpected response: %#v", response)
		}
	}
	if got := bridge.processStartCount(); got != 1 {
		t.Fatalf("process starts = %d, want 1", got)
	}
}

func TestMarpBridgeConcurrentRequestIDs(t *testing.T) {
	bridge := newFakeMarpBridge(t)
	var wait sync.WaitGroup
	errorsByIndex := make(chan error, 8)
	for index := 0; index < 8; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			source := fmt.Sprintf("request-%d", index)
			response, err := bridge.Render(context.Background(), MarpRenderRequest{Source: source})
			if err != nil {
				errorsByIndex <- err
			} else if response.HTML != source {
				errorsByIndex <- fmt.Errorf("request %d received %q", index, response.HTML)
			}
		}(index)
	}
	wait.Wait()
	close(errorsByIndex)
	for err := range errorsByIndex {
		t.Error(err)
	}
	if got := bridge.processStartCount(); got != 1 {
		t.Fatalf("concurrent requests started %d helpers", got)
	}
}

func TestMarpBridgeCancellationDiscardsLateResponse(t *testing.T) {
	bridge := newFakeMarpBridge(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := bridge.Render(ctx, MarpRenderRequest{Source: "slow"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Render error = %v, want deadline exceeded", err)
	}
	time.Sleep(200 * time.Millisecond)
	response, err := bridge.Render(context.Background(), MarpRenderRequest{Source: "current"})
	if err != nil {
		t.Fatal(err)
	}
	if response.HTML != "current" {
		t.Fatalf("late response leaked into current request: %#v", response)
	}
	if got := bridge.processStartCount(); got != 1 {
		t.Fatalf("cancellation restarted process: %d starts", got)
	}
}

func TestMarpBridgeRestartsAfterCrash(t *testing.T) {
	bridge := newFakeMarpBridge(t)
	_, err := bridge.Render(context.Background(), MarpRenderRequest{Source: "crash"})
	if err == nil || !strings.Contains(err.Error(), "intentional helper crash") {
		t.Fatalf("crash error is not actionable: %v", err)
	}
	response, err := bridge.Render(context.Background(), MarpRenderRequest{Source: "recovered"})
	if err != nil {
		t.Fatal(err)
	}
	if response.HTML != "recovered" || bridge.processStartCount() != 2 {
		t.Fatalf("bridge did not restart cleanly: response=%#v starts=%d", response, bridge.processStartCount())
	}
}

func TestMarpBridgeClose(t *testing.T) {
	bridge := newFakeMarpBridge(t)
	if _, err := bridge.Render(context.Background(), MarpRenderRequest{Source: "started"}); err != nil {
		t.Fatal(err)
	}
	if err := bridge.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := bridge.Render(context.Background(), MarpRenderRequest{}); !errors.Is(err, ErrMarpBridgeClosed) {
		t.Fatalf("Render after Close error = %v", err)
	}
}

func TestMarpBridgeMissingNodeAndHelper(t *testing.T) {
	missingHelper := NewMarpBridge(MarpBridgeOptions{HelperPath: t.TempDir() + "/missing.mjs"})
	if _, err := missingHelper.Render(context.Background(), MarpRenderRequest{}); err == nil || !strings.Contains(err.Error(), "helper not found") {
		t.Fatalf("missing helper error = %v", err)
	}
	helper := t.TempDir() + "/helper.mjs"
	if err := os.WriteFile(helper, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	missingNode := NewMarpBridge(MarpBridgeOptions{NodePath: "node-that-does-not-exist-karte", HelperPath: helper})
	if _, err := missingNode.Render(context.Background(), MarpRenderRequest{}); err == nil || !strings.Contains(err.Error(), "start Marp preview helper") {
		t.Fatalf("missing Node error = %v", err)
	}
}

func TestMarpBridgeRejectsNilContext(t *testing.T) {
	bridge := newFakeMarpBridge(t)
	if _, err := bridge.Render(nil, MarpRenderRequest{}); !errors.Is(err, ErrNilMarpContext) {
		t.Fatalf("nil context error = %v", err)
	}
	if bridge.processStartCount() != 0 {
		t.Fatal("nil context started helper process")
	}
}
