package renderer

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
)

// ErrMarpBridgeClosed is returned by Render after Close.
var ErrMarpBridgeClosed = errors.New("Marp preview bridge is closed")

// ErrNilMarpContext is returned when Render receives a nil context.
var ErrNilMarpContext = errors.New("Marp preview context must not be nil")

// MarpBridgeOptions locates the Node runtime and preview helper.
type MarpBridgeOptions struct {
	NodePath   string
	HelperPath string
}

// MarpRenderRequest contains one official Marp Core render request.
type MarpRenderRequest struct {
	Root        string   `json:"root"`
	BaseDir     string   `json:"baseDir,omitempty"`
	Source      string   `json:"source"`
	Theme       string   `json:"theme,omitempty"`
	ThemeSet    []string `json:"themeSet,omitempty"`
	TrustedHTML bool     `json:"trustedHTML,omitempty"`
}

// MarpRenderResponse contains the official Marp Core HTML, CSS, and comments.
type MarpRenderResponse struct {
	HTML     string     `json:"html"`
	CSS      string     `json:"css"`
	Comments [][]string `json:"comments,omitempty"`
	Warnings []string   `json:"warnings,omitempty"`
}

type marpWireRequest struct {
	ID string `json:"id"`
	MarpRenderRequest
}

type marpWireResponse struct {
	ID     string             `json:"id"`
	Result MarpRenderResponse `json:"result"`
	Error  string             `json:"error,omitempty"`
}

type marpReply struct {
	response marpWireResponse
	err      error
}

type marpPending struct {
	process *marpProcess
	channel chan marpReply
}

type marpProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	done   chan struct{}
	stderr bytes.Buffer
}

// MarpBridge owns one lazily started, reusable Node helper process.
type MarpBridge struct {
	mu      sync.Mutex
	options MarpBridgeOptions
	process *marpProcess
	pending map[string]marpPending
	nextID  uint64
	closed  bool
	starts  int
	command func(string, ...string) *exec.Cmd
}

// NewMarpBridge constructs a lazy Marp Core bridge.
func NewMarpBridge(options ...MarpBridgeOptions) *MarpBridge {
	var opts MarpBridgeOptions
	if len(options) > 0 {
		opts = options[0]
	}
	if opts.NodePath == "" {
		opts.NodePath = "node"
	}
	if opts.HelperPath == "" {
		opts.HelperPath = defaultMarpHelperPath()
	}
	return &MarpBridge{options: opts, pending: make(map[string]marpPending), command: exec.Command}
}

func defaultMarpHelperPath() string {
	if value := os.Getenv("KARTE_MARP_PREVIEW_HELPER"); value != "" {
		return value
	}
	if _, filename, _, ok := runtime.Caller(0); ok {
		candidate := filepath.Join(filepath.Dir(filename), "scripts", "marp-preview.mjs")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	workingCandidate := filepath.Join("scripts", "marp-preview.mjs")
	if _, err := os.Stat(workingCandidate); err == nil {
		return workingCandidate
	}
	if executable, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(executable), "scripts", "marp-preview.mjs")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return workingCandidate
}

// Render renders one request and safely correlates its response by request ID.
func (b *MarpBridge) Render(ctx context.Context, request MarpRenderRequest) (MarpRenderResponse, error) {
	if ctx == nil {
		return MarpRenderResponse{}, ErrNilMarpContext
	}
	if err := ctx.Err(); err != nil {
		return MarpRenderResponse{}, err
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return MarpRenderResponse{}, ErrMarpBridgeClosed
	}
	process, err := b.startLocked()
	if err != nil {
		b.mu.Unlock()
		return MarpRenderResponse{}, err
	}
	b.nextID++
	id := strconv.FormatUint(b.nextID, 10)
	channel := make(chan marpReply, 1)
	b.pending[id] = marpPending{process: process, channel: channel}
	payload, _ := json.Marshal(marpWireRequest{ID: id, MarpRenderRequest: request})
	payload = append(payload, '\n')
	if _, err = process.stdin.Write(payload); err != nil {
		delete(b.pending, id)
		b.failProcessLocked(process, fmt.Errorf("write request: %w", err))
		b.mu.Unlock()
		return MarpRenderResponse{}, fmt.Errorf("Marp preview helper is unavailable: %w", err)
	}
	b.mu.Unlock()

	select {
	case reply := <-channel:
		if err := ctx.Err(); err != nil {
			return MarpRenderResponse{}, err
		}
		if reply.err != nil {
			return MarpRenderResponse{}, reply.err
		}
		if reply.response.Error != "" {
			return MarpRenderResponse{}, fmt.Errorf("Marp preview: %s", reply.response.Error)
		}
		return reply.response.Result, nil
	case <-ctx.Done():
		b.mu.Lock()
		if pending, ok := b.pending[id]; ok && pending.channel == channel {
			delete(b.pending, id)
		}
		b.mu.Unlock()
		return MarpRenderResponse{}, ctx.Err()
	}
}

func (b *MarpBridge) startLocked() (*marpProcess, error) {
	if b.process != nil {
		return b.process, nil
	}
	info, err := os.Stat(b.options.HelperPath)
	if err != nil {
		return nil, fmt.Errorf("Marp preview helper not found at %s: %w", b.options.HelperPath, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("Marp preview helper is a directory: %s", b.options.HelperPath)
	}
	cmd := b.command(b.options.NodePath, b.options.HelperPath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open Marp preview helper stdout: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open Marp preview helper stdin: %w", err)
	}
	process := &marpProcess{cmd: cmd, stdin: stdin, done: make(chan struct{})}
	cmd.Stderr = &process.stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start Marp preview helper with %q: %w", b.options.NodePath, err)
	}
	b.process = process
	b.starts++
	go b.readProcess(process, stdout)
	return process, nil
}

func (b *MarpBridge) readProcess(process *marpProcess, stdout io.Reader) {
	var protocolErr error
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 64*1024*1024)
	for scanner.Scan() {
		var response marpWireResponse
		if err := json.Unmarshal(scanner.Bytes(), &response); err != nil || response.ID == "" {
			if err == nil {
				err = errors.New("response has no request id")
			}
			protocolErr = fmt.Errorf("invalid helper response: %w", err)
			_ = process.cmd.Process.Kill()
			break
		}
		b.mu.Lock()
		if pending, ok := b.pending[response.ID]; ok && pending.process == process {
			delete(b.pending, response.ID)
			pending.channel <- marpReply{response: response}
		}
		b.mu.Unlock()
	}
	if err := scanner.Err(); err != nil && protocolErr == nil {
		protocolErr = fmt.Errorf("read helper response: %w", err)
	}
	waitErr := process.cmd.Wait()
	if protocolErr == nil {
		if waitErr != nil {
			protocolErr = fmt.Errorf("Marp preview helper exited unexpectedly: %w", waitErr)
		} else {
			protocolErr = errors.New("Marp preview helper exited unexpectedly")
		}
	}
	if protocolErr != nil && process.stderr.Len() > 0 {
		protocolErr = fmt.Errorf("%w: %s", protocolErr, bytes.TrimSpace(process.stderr.Bytes()))
	}
	b.mu.Lock()
	if b.process == process {
		b.process = nil
	}
	for id, pending := range b.pending {
		if pending.process == process {
			delete(b.pending, id)
			pending.channel <- marpReply{err: protocolErr}
		}
	}
	b.mu.Unlock()
	close(process.done)
}

func (b *MarpBridge) failProcessLocked(process *marpProcess, cause error) {
	if b.process == process {
		b.process = nil
	}
	for id, pending := range b.pending {
		if pending.process == process {
			delete(b.pending, id)
			pending.channel <- marpReply{err: cause}
		}
	}
	_ = process.cmd.Process.Kill()
}

// Close terminates the helper process and rejects future renders.
func (b *MarpBridge) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	process := b.process
	b.process = nil
	for id, pending := range b.pending {
		delete(b.pending, id)
		pending.channel <- marpReply{err: ErrMarpBridgeClosed}
	}
	b.mu.Unlock()
	if process == nil {
		return nil
	}
	err := process.cmd.Process.Kill()
	<-process.done
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}

func (b *MarpBridge) processStartCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.starts
}
