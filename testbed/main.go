package main

import (
	"context"
	"github.com/ObolNetwork/kurtosis-charon/testbed/cmd"
	"github.com/obolnetwork/charon/app/log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	ctx = log.WithTopic(ctx, "cmd")

	err := cmd.New().ExecuteContext(ctx)

	cancel()

	if err != nil {
		log.Error(ctx, "Fatal error", err)
		os.Exit(1)
	}

	//err := startLocalPrometheus(ctx)
	//if err != nil {
	//	panic(err)
	//}
	//
	//time.Sleep(3 * time.Second)
	//
	//err = stopLocalPrometheus(ctx)
	//if err != nil {
	//	panic(err)
	//}

	//if err := kurtosis.DefinitionServer(); err != nil {
	//	panic(err)
	//}
}
