package main

import (
	"github.com/alxark/udp-to-metrics/internal"
	"log"
)

// main function, create Application instance and run it
func main() {
	logger := log.Default()
	app, err := internal.NewApplication(logger)
	if err != nil {
		logger.Fatal("failed to create application: ", err.Error())
	}

	if err := app.Run(); err != nil {
		logger.Fatal("run failed: ", err.Error())
	}
}
