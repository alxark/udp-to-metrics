//go:build linux || darwin

package main

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
)

func TestExitWithSignalHelper(t *testing.T) {
	if os.Getenv("UDP_TO_METRICS_EXIT_HELPER") != "1" {
		return
	}
	exitWithSignal(syscall.SIGTERM)
}

func TestExitWithSignal(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestExitWithSignalHelper")
	cmd.Env = append(os.Environ(), "UDP_TO_METRICS_EXIT_HELPER=1")
	err := cmd.Run()
	if err == nil {
		t.Fatal("helper unexpectedly exited successfully")
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("unexpected error type: %T", err)
	}
	status, ok := exitErr.ProcessState.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGTERM {
		t.Fatalf("expected SIGTERM termination, got %v", status)
	}
}
