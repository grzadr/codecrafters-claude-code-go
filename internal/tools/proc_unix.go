//go:build unix

package tools

import (
	"os/exec"
	"syscall"
)

func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		// Negative PID targets the entire process group
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
}
