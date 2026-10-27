//go:build windows

package renderer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

func isolateProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.Cancel = func() error {
		killProcessTree(cmd)
		return nil
	}
}

func killProcessTree(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		// Chromium spawns renderer and utility children. Killing only the parent
		// leaves those processes holding the temporary profile and output pipes.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		taskkill := filepath.Join(os.Getenv("SystemRoot"), "System32", "taskkill.exe")
		kill := exec.CommandContext(ctx, taskkill, "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
		kill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		_ = kill.Run()
		_ = cmd.Process.Kill()
	}
}
