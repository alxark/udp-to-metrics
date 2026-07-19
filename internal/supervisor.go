//go:build linux || darwin

package internal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

const groupPollInterval = 25 * time.Millisecond

type ChildResult struct {
	Code   int
	Signal syscall.Signal
}

type Supervisor struct {
	log         *log.Logger
	killTimeout time.Duration
	stdout      io.Writer
	stderr      io.Writer
}

type childWaitResult struct {
	result ChildResult
	err    error
}

func NewSupervisor(log *log.Logger, killTimeout time.Duration, stdout, stderr io.Writer) *Supervisor {
	return &Supervisor{log: log, killTimeout: killTimeout, stdout: stdout, stderr: stderr}
}

func (s *Supervisor) Run(ctx context.Context, argv []string) (ChildResult, error) {
	if len(argv) == 0 {
		return ChildResult{}, errors.New("child command is empty")
	}
	if err := ctx.Err(); err != nil {
		return ChildResult{}, err
	}

	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)

	log.Printf("starting child process: %s", argv[0])
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdout = s.stdout
	cmd.Stderr = s.stderr
	configureProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return ChildResult{}, fmt.Errorf("start child: %w", err)
	}

	pid := cmd.Process.Pid
	waitDone := make(chan childWaitResult, 1)
	go func() {
		waitDone <- waitForChild(cmd)
	}()

	var (
		waited             bool
		waitResult         childWaitResult
		terminationStarted bool
		timer              *time.Timer
		timerC             <-chan time.Time
		poll               *time.Ticker
		pollC              <-chan time.Time
		ctxDone            = ctx.Done()
	)
	defer func() {
		if timer != nil {
			timer.Stop()
		}
		if poll != nil {
			poll.Stop()
		}
	}()

	beginTermination := func(sig syscall.Signal) {
		if err := signalProcessGroup(pid, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
			s.log.Printf("failed to signal child process group: %s", err)
		}
		if terminationStarted {
			return
		}
		terminationStarted = true
		if s.killTimeout > 0 {
			timer = time.NewTimer(s.killTimeout)
			timerC = timer.C
		}
	}

	for {
		select {
		case sig := <-signals:
			unixSignal, ok := sig.(syscall.Signal)
			if ok {
				beginTermination(unixSignal)
			}
		case <-ctxDone:
			ctxDone = nil
			beginTermination(syscall.SIGTERM)
		case waitResult = <-waitDone:
			waited = true
			waitDone = nil
			if !terminationStarted || !processGroupAlive(pid) {
				return waitResult.result, waitResult.err
			}
			poll = time.NewTicker(groupPollInterval)
			pollC = poll.C
		case <-pollC:
			if !processGroupAlive(pid) {
				return waitResult.result, waitResult.err
			}
		case <-timerC:
			timerC = nil
			if err := signalProcessGroup(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
				s.log.Printf("failed to kill child process group: %s", err)
			}
			if waited && !processGroupAlive(pid) {
				return waitResult.result, waitResult.err
			}
		}
	}
}

func waitForChild(cmd *exec.Cmd) childWaitResult {
	err := cmd.Wait()
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return childWaitResult{err: fmt.Errorf("wait for child: %w", err)}
		}
	}

	status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
	if !ok {
		return childWaitResult{err: errors.New("child returned an unsupported wait status")}
	}
	if status.Signaled() {
		return childWaitResult{result: ChildResult{Signal: status.Signal()}}
	}
	return childWaitResult{result: ChildResult{Code: status.ExitStatus()}}
}
