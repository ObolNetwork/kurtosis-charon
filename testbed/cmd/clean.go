package cmd

import (
	"context"
	"github.com/ObolNetwork/kurtosis-charon/testbed/kurtosis"
	"github.com/obolnetwork/charon/app/errors"
	"github.com/obolnetwork/charon/app/log"
	"github.com/spf13/cobra"
)

type cleanConfig struct {
	Log log.Config
}

func newCleanCmd(runFunc func(context.Context) error) *cobra.Command {
	var conf cleanConfig

	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Delete all Kurtosis-related Docker entities.",
		Long:  "Starts the long-running Charon middleware process to perform distributed validator duties.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := log.InitLogger(conf.Log); err != nil {
				return err
			}

			printFlags(cmd.Context(), cmd.Flags())

			return runFunc(cmd.Context())
		},
	}

	bindLogFlags(cmd.Flags(), &conf.Log)

	return cmd
}

func runCleanCmd(ctx context.Context) error {
	if err := kurtosis.ClearContainers(ctx); err != nil {
		return errors.Wrap(err, "cannot stop and remove kurtosis containers")
	}

	return nil
}
