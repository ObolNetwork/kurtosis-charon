package main

import (
	"github.com/obol/kurtosis-charon/cmd"
	"github.com/sirupsen/logrus"
)

func main() {
	// Configure logging
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	// Execute the root command
	cmd.Execute()
}
