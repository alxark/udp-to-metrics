//go:build linux || darwin

package internal

import (
	"errors"
	"os/exec"
	"syscall"
)

func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func signalProcessGroup(pid int, sig syscall.Signal) error {
	return syscall.Kill(-pid, sig)
}

func processGroupAlive(pid int) bool {
	err := signalProcessGroup(pid, 0)
	return err == nil || !errors.Is(err, syscall.ESRCH)
}
