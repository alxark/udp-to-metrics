package main

import (
	"github.com/alxark/udp-to-metrics/internal"
	"log"
	"os"
)

// main function, create Application instance and run it
func main() {
	logger := log.Default()
	app, err := internal.NewApplication(logger)
	if err != nil {
		logger.Fatal("failed to create application: ", err.Error())
	}

	result, err := app.Run(os.Args[1:])
	if err != nil {
		logger.Print("run failed: ", err.Error())
		os.Exit(1)
	}

	if result.Signal != 0 {
		exitWithSignal(result.Signal)
	}

	os.Exit(result.Code)
}
