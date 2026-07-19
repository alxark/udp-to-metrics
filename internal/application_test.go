package internal

import (
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestReadFlags(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantError string
		check     func(*testing.T, *Application)
	}{
		{
			name: "daemon defaults",
			args: []string{"-metricsFile=metrics.json"},
			check: func(t *testing.T, app *Application) {
				if app.HttpPort != 8080 || app.UdpPort != 9090 {
					t.Fatalf("unexpected default ports: HTTP=%d UDP=%d", app.HttpPort, app.UdpPort)
				}
				if app.Supervise || app.SuperviseKillTimeout != 30*time.Second {
					t.Fatalf("unexpected supervise defaults: enabled=%t timeout=%s", app.Supervise, app.SuperviseKillTimeout)
				}
			},
		},
		{
			name: "supervise command",
			args: []string{"-metricsFile=metrics.json", "-supervise", "-superviseKillTimeout=0", "--", "/bin/service", "--child-flag"},
			check: func(t *testing.T, app *Application) {
				if !app.Supervise || app.SuperviseKillTimeout != 0 {
					t.Fatalf("unexpected supervise configuration: enabled=%t timeout=%s", app.Supervise, app.SuperviseKillTimeout)
				}
				if got := strings.Join(app.ChildArgs, " "); got != "/bin/service --child-flag" {
					t.Fatalf("unexpected child arguments: %q", got)
				}
			},
		},
		{name: "missing delimiter", args: []string{"-supervise", "child"}, wantError: "requires --"},
		{name: "missing command", args: []string{"-supervise", "--"}, wantError: "requires a child command"},
		{name: "daemon delimiter", args: []string{"--", "child"}, wantError: "only valid with -supervise"},
		{name: "daemon positional argument", args: []string{"child"}, wantError: "positional arguments"},
		{name: "negative timeout", args: []string{"-supervise", "-superviseKillTimeout=-1s", "--", "child"}, wantError: "cannot be negative"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app, err := NewApplication(log.New(io.Discard, "", 0))
			if err != nil {
				t.Fatal(err)
			}
			err = app.readFlags(test.args)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("expected error containing %q, got %v", test.wantError, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if test.check != nil {
				test.check(t, app)
			}
		})
	}
}

func TestApplicationSuperviseReturnsChildExitCode(t *testing.T) {
	metricsFile := writeMetricsFile(t)
	namespace := "application_test_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	app, err := NewApplication(log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}

	result, err := app.Run([]string{
		"-metricsFile=" + metricsFile,
		"-httpPort=0",
		"-udpPort=0",
		"-defaultNamespace=" + namespace,
		"-supervise",
		"--",
		os.Args[0], "-udp-helper", "exit", "17", "quiet",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != 17 || result.Signal != 0 {
		t.Fatalf("unexpected child result: %+v", result)
	}
}

func TestApplicationDoesNotStartChildWhenHTTPBindFails(t *testing.T) {
	lis, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer lis.Close()

	metricsFile := writeMetricsFile(t)
	port := lis.Addr().(*net.TCPAddr).Port
	app, err := NewApplication(log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.Run([]string{
		"-metricsFile=" + metricsFile,
		"-httpPort=" + strconv.Itoa(port),
		"-udpPort=0",
		"-defaultNamespace=bind_failure_" + strconv.FormatInt(time.Now().UnixNano(), 10),
		"-supervise", "--", "/definitely/not/a/command",
	})
	if err == nil || !strings.Contains(err.Error(), "bind HTTP listener") {
		t.Fatalf("expected HTTP bind error before child startup, got %v", err)
	}
}

func TestApplicationUDPBindFailureRollsBackReceiver(t *testing.T) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	namespace := "udp_rollback_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	metricsFile := writeMetricsFile(t)
	app, err := NewApplication(log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.Run([]string{
		"-metricsFile=" + metricsFile,
		"-httpPort=0",
		"-udpPort=" + strconv.Itoa(conn.LocalAddr().(*net.UDPAddr).Port),
		"-defaultNamespace=" + namespace,
		"-supervise", "--", "/definitely/not/a/command",
	})
	if err == nil || !strings.Contains(err.Error(), "bind UDP listener") {
		t.Fatalf("expected UDP bind error before child startup, got %v", err)
	}

	receiver, err := NewReceiver(log.New(io.Discard, "", 0), namespace, "app", 0, nil)
	if err != nil {
		t.Fatalf("receiver collectors were not rolled back: %v", err)
	}
	if err := receiver.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeMetricsFile(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + "/metrics.json"
	if err := os.WriteFile(path, []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
