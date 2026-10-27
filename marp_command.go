package renderer

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// npm installs both a POSIX script and a .cmd shim on Windows. Neither is a
// native executable. Run the official JavaScript entry point with Node so
// paths and arguments never pass through cmd.exe's expansion rules.
func marpCommand(ctx context.Context, binary string, args ...string) (*exec.Cmd, error) {
	if runtime.GOOS != "windows" {
		return exec.CommandContext(ctx, binary, args...), nil
	}
	if resolved, err := exec.LookPath(binary); err == nil {
		binary = resolved
	}
	if strings.ContainsAny(binary, `/\`) {
		absolute, err := filepath.Abs(binary)
		if err != nil {
			return nil, fmt.Errorf("resolve Marp launcher: %w", err)
		}
		binary = absolute
	}
	entry := binary
	switch strings.ToLower(filepath.Ext(binary)) {
	case ".cmd", ".bat":
		dir := filepath.Dir(binary)
		entry = ""
		for _, candidate := range []string{
			filepath.Join(dir, "..", "@marp-team", "marp-cli", "marp-cli.js"),
			filepath.Join(dir, "node_modules", "@marp-team", "marp-cli", "marp-cli.js"),
		} {
			if regularFileExists(candidate) {
				entry = candidate
				break
			}
		}
		if entry == "" {
			return nil, fmt.Errorf("cannot resolve Marp npm launcher %s; set MARP_BINARY to marp-cli.js or a native executable", binary)
		}
	case ".js", ".cjs", ".mjs":
	default:
		return exec.CommandContext(ctx, binary, args...), nil
	}
	node := filepath.Join(filepath.Dir(binary), "node.exe")
	if !regularFileExists(node) {
		var err error
		node, err = exec.LookPath("node")
		if err != nil {
			return nil, fmt.Errorf("Node.js is required to run the Marp npm package: %w", err)
		}
	}
	return exec.CommandContext(ctx, node, append([]string{entry}, args...)...), nil
}
