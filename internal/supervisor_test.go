//go:build linux || darwin

package internal

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	for i, arg := range os.Args {
		if arg == "-udp-helper" && i+1 < len(os.Args) {
			runSupervisorHelper(os.Args[i+1:])
			return
		}
	}
	os.Exit(m.Run())
}

func runSupervisorHelper(args []string) {
	switch args[0] {
	case "exit":
		code, err := strconv.Atoi(args[1])
		if err != nil {
			os.Exit(125)
		}
		if len(args) < 3 || args[2] != "quiet" {
			fmt.Fprintln(os.Stdout, "child stdout")
			fmt.Fprintln(os.Stderr, "child stderr")
		}
		os.Exit(code)
	case "wait":
		fmt.Fprintf(os.Stdout, "ready %d\n", os.Getpid())
		for {
			time.Sleep(time.Hour)
		}
	case "ignore-term":
		signal.Ignore(syscall.SIGTERM)
		fmt.Fprintf(os.Stdout, "ready %d\n", os.Getpid())
		for {
			time.Sleep(time.Hour)
		}
	}
}

func TestSupervisorPreservesExitCodeAndStreams(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	supervisor := NewSupervisor(log.New(io.Discard, "", 0), time.Second, &stdout, &stderr)
	result, err := supervisor.Run(context.Background(), helperCommand("exit", "17"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != 17 || result.Signal != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if stdout.String() != "child stdout\n" {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
	if stderr.String() != "child stderr\n" {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestSupervisorContextCancellationTerminatesChild(t *testing.T) {
	result := runCancellableHelper(t, "wait", time.Second)
	if result.Signal != syscall.SIGTERM {
		t.Fatalf("expected SIGTERM, got %+v", result)
	}
}

func TestSupervisorForwardsReceivedSignal(t *testing.T) {
	stdoutReader, stdoutWriter := io.Pipe()
	defer stdoutReader.Close()
	defer stdoutWriter.Close()

	supervisor := NewSupervisor(log.New(io.Discard, "", 0), time.Second, stdoutWriter, io.Discard)
	done := make(chan childWaitResult, 1)
	go func() {
		result, err := supervisor.Run(context.Background(), helperCommand("wait"))
		done <- childWaitResult{result: result, err: err}
	}()
	readReadyPID(t, stdoutReader)
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}

	select {
	case outcome := <-done:
		if outcome.err != nil {
			t.Fatal(outcome.err)
		}
		if outcome.result.Signal != syscall.SIGINT {
			t.Fatalf("expected SIGINT, got %+v", outcome.result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("supervisor did not forward SIGINT")
	}
}

func TestSupervisorEscalatesAfterTimeout(t *testing.T) {
	started := time.Now()
	result := runCancellableHelper(t, "ignore-term", 100*time.Millisecond)
	if result.Signal != syscall.SIGKILL {
		t.Fatalf("expected SIGKILL, got %+v", result)
	}
	if elapsed := time.Since(started); elapsed < 50*time.Millisecond || elapsed > time.Second {
		t.Fatalf("unexpected escalation duration: %s", elapsed)
	}
}

func TestSupervisorZeroTimeoutDoesNotEscalate(t *testing.T) {
	stdoutReader, stdoutWriter := io.Pipe()
	defer stdoutReader.Close()
	defer stdoutWriter.Close()

	ctx, cancel := context.WithCancel(context.Background())
	supervisor := NewSupervisor(log.New(io.Discard, "", 0), 0, stdoutWriter, io.Discard)
	done := make(chan childWaitResult, 1)
	go func() {
		result, err := supervisor.Run(ctx, helperCommand("ignore-term"))
		done <- childWaitResult{result: result, err: err}
	}()
	pid := readReadyPID(t, stdoutReader)
	cancel()

	select {
	case outcome := <-done:
		t.Fatalf("zero timeout unexpectedly stopped child: %+v", outcome)
	case <-time.After(150 * time.Millisecond):
	}
	if err := signalProcessGroup(pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	select {
	case outcome := <-done:
		if outcome.err != nil {
			t.Fatal(outcome.err)
		}
		if outcome.result.Signal != syscall.SIGKILL {
			t.Fatalf("unexpected cleanup result: %+v", outcome.result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("supervisor did not reap manually killed child")
	}
}

func TestSupervisorStartFailure(t *testing.T) {
	supervisor := NewSupervisor(log.New(io.Discard, "", 0), time.Second, io.Discard, io.Discard)
	_, err := supervisor.Run(context.Background(), []string{"/definitely/not/a/command"})
	if err == nil {
		t.Fatal("expected child start error")
	}
}

func runCancellableHelper(t *testing.T, mode string, timeout time.Duration) ChildResult {
	t.Helper()
	stdoutReader, stdoutWriter := io.Pipe()
	defer stdoutReader.Close()
	defer stdoutWriter.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	supervisor := NewSupervisor(log.New(io.Discard, "", 0), timeout, stdoutWriter, io.Discard)
	done := make(chan childWaitResult, 1)
	go func() {
		result, err := supervisor.Run(ctx, helperCommand(mode))
		done <- childWaitResult{result: result, err: err}
	}()

	readReadyPID(t, stdoutReader)
	cancel()

	select {
	case outcome := <-done:
		if outcome.err != nil {
			t.Fatal(outcome.err)
		}
		return outcome.result
	case <-time.After(3 * time.Second):
		t.Fatal("supervisor did not stop")
		return ChildResult{}
	}
}

func readReadyPID(t *testing.T, reader io.Reader) int {
	t.Helper()
	ready := make(chan struct {
		pid int
		err error
	}, 1)
	go func() {
		line, err := bufio.NewReader(reader).ReadString('\n')
		pid := 0
		if err == nil {
			_, err = fmt.Sscanf(line, "ready %d\n", &pid)
		}
		ready <- struct {
			pid int
			err error
		}{pid: pid, err: err}
	}()
	select {
	case result := <-ready:
		if result.err != nil {
			t.Fatal(result.err)
		}
		return result.pid
	case <-time.After(2 * time.Second):
		t.Fatal("child did not become ready")
		return 0
	}
}

func helperCommand(args ...string) []string {
	command := []string{os.Args[0], "-udp-helper"}
	return append(command, args...)
}
