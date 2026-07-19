//go:build linux || darwin

package main

import (
	"os"
	"os/signal"
	"syscall"
)

func exitWithSignal(sig syscall.Signal) {
	signal.Reset(sig)
	if err := syscall.Kill(syscall.Getpid(), sig); err != nil {
		os.Exit(128 + int(sig))
	}

	// A successfully delivered fatal signal should make this unreachable.
	select {}
}
