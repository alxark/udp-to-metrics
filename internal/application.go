package internal

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"syscall"
	"time"
)

type RunResult struct {
	Code   int
	Signal syscall.Signal
}

type Application struct {
	log                  *log.Logger
	HttpPort             int
	UdpPort              int
	DefaultNamespace     string
	DefaultSubsystem     string
	MetricsFile          string
	Supervise            bool
	SuperviseKillTimeout time.Duration
	ChildArgs            []string
}

type supervisorResult struct {
	result ChildResult
	err    error
}

func NewApplication(log *log.Logger) (*Application, error) {
	return &Application{log: log}, nil
}

func (a *Application) Run(args []string) (RunResult, error) {
	if err := a.readFlags(args); err != nil {
		return RunResult{}, err
	}
	if a.MetricsFile == "" {
		return RunResult{}, errors.New("metrics file is not specified, use -metricsFile flag")
	}

	data, err := os.ReadFile(a.MetricsFile)
	if err != nil {
		return RunResult{}, fmt.Errorf("open metrics file %q: %w", a.MetricsFile, err)
	}
	var metrics []Metric
	if err := json.Unmarshal(data, &metrics); err != nil {
		return RunResult{}, fmt.Errorf("decode metrics file %q: %w", a.MetricsFile, err)
	}

	a.log.Printf("initializing receiver. Default namespace: %s, Default subsystem: %s", a.DefaultNamespace, a.DefaultSubsystem)
	rcvr, err := NewReceiver(a.log, a.DefaultNamespace, a.DefaultSubsystem, a.UdpPort, metrics)
	if err != nil {
		return RunResult{}, fmt.Errorf("initialize receiver: %w", err)
	}
	websrv, err := NewHttpServer(a.log, a.HttpPort)
	if err != nil {
		return RunResult{}, fmt.Errorf("initialize HTTP server: %w", err)
	}
	if err := websrv.Listen(); err != nil {
		_ = rcvr.Close()
		return RunResult{}, fmt.Errorf("bind HTTP listener: %w", err)
	}
	if err := rcvr.Listen(); err != nil {
		_ = websrv.Close()
		_ = rcvr.Close()
		return RunResult{}, fmt.Errorf("bind UDP listener: %w", err)
	}

	receiverCtx, cancelReceiver := context.WithCancel(context.Background())
	workerErr := make(chan error, 2)
	var workers sync.WaitGroup
	workers.Add(2)
	go runWorker(&workers, workerErr, "HTTP server", websrv.Run)
	go runWorker(&workers, workerErr, "UDP receiver", func() error { return rcvr.Run(receiverCtx) })

	shutdown := func() {
		cancelReceiver()
		_ = rcvr.Close()
		_ = websrv.Close()
		workers.Wait()
	}

	if !a.Supervise {
		err := <-workerErr
		shutdown()
		return RunResult{}, err
	}

	supervisorCtx, cancelSupervisor := context.WithCancel(context.Background())
	defer cancelSupervisor()
	supervisorDone := make(chan supervisorResult, 1)
	supervisor := NewSupervisor(a.log, a.SuperviseKillTimeout, os.Stdout, os.Stderr)
	go func() {
		result, err := supervisor.Run(supervisorCtx, a.ChildArgs)
		supervisorDone <- supervisorResult{result: result, err: err}
	}()

	var child supervisorResult
	select {
	case child = <-supervisorDone:
		select {
		case err := <-workerErr:
			cancelSupervisor()
			shutdown()
			return RunResult{}, err
		default:
		}
	case err := <-workerErr:
		cancelSupervisor()
		<-supervisorDone
		shutdown()
		return RunResult{}, err
	}

	shutdown()
	if child.err != nil {
		return RunResult{}, child.err
	}
	return RunResult{Code: child.result.Code, Signal: child.result.Signal}, nil
}

func runWorker(wg *sync.WaitGroup, result chan<- error, name string, run func() error) {
	defer wg.Done()
	if err := run(); err != nil {
		result <- fmt.Errorf("%s failed: %w", name, err)
		return
	}
	result <- fmt.Errorf("%s stopped unexpectedly", name)
}

func (a *Application) readFlags(args []string) error {
	delimiter := -1
	for i, arg := range args {
		if arg == "--" {
			delimiter = i
			break
		}
	}

	flagArgs := args
	if delimiter >= 0 {
		flagArgs = args[:delimiter]
		a.ChildArgs = append([]string(nil), args[delimiter+1:]...)
	} else {
		a.ChildArgs = nil
	}

	flags := flag.NewFlagSet("udp-to-metrics", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.IntVar(&a.HttpPort, "httpPort", 8080, "HTTP port used to read metrics")
	flags.IntVar(&a.UdpPort, "udpPort", 9090, "UDP port used to receive messages")
	flags.StringVar(&a.DefaultNamespace, "defaultNamespace", "utm", "default prometheus namespace")
	flags.StringVar(&a.DefaultSubsystem, "defaultSubsystem", "app", "default prometheus subsystem")
	flags.StringVar(&a.MetricsFile, "metricsFile", "", "metrics file, should be a JSON file")
	flags.BoolVar(&a.Supervise, "supervise", false, "run and supervise a child process")
	flags.DurationVar(&a.SuperviseKillTimeout, "superviseKillTimeout", 30*time.Second, "delay before killing the supervised process group")
	if err := flags.Parse(flagArgs); err != nil {
		return err
	}
	if len(flags.Args()) != 0 {
		if a.Supervise && delimiter < 0 {
			return errors.New("supervise mode requires -- before the child command")
		}
		return errors.New("positional arguments are not supported")
	}
	if a.SuperviseKillTimeout < 0 {
		return errors.New("superviseKillTimeout cannot be negative")
	}
	if a.Supervise {
		if delimiter < 0 {
			return errors.New("supervise mode requires -- before the child command")
		}
		if len(a.ChildArgs) == 0 {
			return errors.New("supervise mode requires a child command after -- sign")
		}
		return nil
	}
	if delimiter >= 0 {
		return errors.New("-- is only valid with -supervise")
	}
	return nil
}
