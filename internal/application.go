package internal

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io/ioutil"
	"log"
	"os"
	"sync"
)

type Application struct {
	log              *log.Logger
	HttpPort         int
	UdpPort          int
	DefaultNamespace string
	DefaultSubsystem string
	MetricsFile      string
}

func NewApplication(log *log.Logger) (*Application, error) {
	return &Application{
		log: log,
	}, nil
}

func (a *Application) Run() (err error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err = a.readFlags(); err != nil {
		return err
	}

	if a.MetricsFile == "" {
		a.log.Printf("metrics file is not specified")
		return errors.New("metrics file is not specified, use -metricsFile flag")
	}

	metricsFile, err := os.Open(a.MetricsFile)
	if err != nil {
		a.log.Printf("failed to open metrics file: %s, got: %s", a.MetricsFile, err.Error())
		return err
	}

	data, err := ioutil.ReadAll(metricsFile)
	if err != nil {
		return err
	}

	var metrics []Metric
	err = json.Unmarshal(data, &metrics)
	if err != nil {
		return err
	}

	websrv, err := NewHttpServer(a.log, a.HttpPort)
	if err != nil {
		return err
	}

	a.log.Printf("initializing receiver. Default namespace: %s, Default subsystem: %s", a.DefaultNamespace, a.DefaultSubsystem)

	rcvr, err := NewReceiver(a.log, a.DefaultNamespace, a.DefaultSubsystem, a.UdpPort, metrics)
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := websrv.Run(); err != nil {
			a.log.Fatal("failed to run http server: ", err.Error())
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := rcvr.Run(ctx); err != nil {
			a.log.Fatal("failed to run receiver: ", err.Error())
		}
	}()

	wg.Wait()

	return nil
}

func (a *Application) readFlags() error {
	flag.IntVar(&a.HttpPort, "httpPort", 8080, "HTTP port used to read metrics")
	flag.IntVar(&a.UdpPort, "udpPort", 9090, "UDP port used to receive messages")
	flag.StringVar(&a.DefaultNamespace, "defaultNamespace", "utm", "default prometheus namespace")
	flag.StringVar(&a.DefaultSubsystem, "defaultSubsystem", "app", "default prometheus subsystem")
	flag.StringVar(&a.MetricsFile, "metricsFile", "", "metrics file, should be a JSON file")

	flag.Parse()

	return nil
}
