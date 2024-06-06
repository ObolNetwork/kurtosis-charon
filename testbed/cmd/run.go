package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/ObolNetwork/kurtosis-charon/testbed/kurtosis"
	"github.com/ObolNetwork/kurtosis-charon/testbed/model"
	"github.com/kurtosis-tech/kurtosis/api/golang/core/kurtosis_core_rpc_api_bindings"
	"github.com/kurtosis-tech/kurtosis/api/golang/core/lib/enclaves"
	"github.com/kurtosis-tech/kurtosis/api/golang/core/lib/starlark_run_config"
	"github.com/obolnetwork/charon/app/errors"
	"github.com/obolnetwork/charon/app/log"
	"github.com/obolnetwork/charon/app/z"
	"github.com/spf13/cobra"
	"os"
)

type runConfig struct {
	Path string

	Log log.Config
}

func newRunCmd(runFunc func(context.Context, runConfig) error) *cobra.Command {
	var conf runConfig

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the charon middleware client",
		Long:  "Starts the long-running Charon middleware process to perform distributed validator duties.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := log.InitLogger(conf.Log); err != nil {
				return err
			}

			printFlags(cmd.Context(), cmd.Flags())

			return runFunc(cmd.Context(), conf)
		},
	}

	bindLogFlags(cmd.Flags(), &conf.Log)

	cmd.Flags().StringVar(&conf.Path, "path", "", "Testbed configuration file.")
	if err := cmd.MarkFlagRequired("path"); err != nil {
		panic(err) // Panic is ok since this is unexpected and covered by unit tests.
	}

	return cmd
}

func newRunCmdFunc(ctx context.Context, config runConfig) (finalErr error) {
	configFile, err := os.Open(config.Path)
	if err != nil {
		return errors.Wrap(err, "can't open configuration path")
	}

	var definition RunConfig

	if err := json.NewDecoder(configFile).Decode(&definition); err != nil {
		return errors.Wrap(err, "can't decode definition file")
	}

	if err := kurtosis.Stop(ctx); err != nil {
		return errors.Wrap(err, "can't preemptively stop kurtosis")
	}

	_, encCtx, err := kurtosis.Start(ctx)
	if err != nil {
		return errors.Wrap(err, "cannot start kurtosis")
	}

	defer func() {
		if err := kurtosis.Stop(ctx); err != nil {
			finalErr = errors.Wrap(err, "cannot stop kurtosis")
		}
	}()

	perms := definition.Permutations()

	for idx, perm := range perms {
		if err := perm.Validate(); err != nil {
			return errors.Wrap(err, "invalid network configuration", z.Int("idx", idx), z.Any("configuration", perm))
		}

		log.Debug(ctx, "network definition", z.Str("network_definition", fmt.Sprintf("%+v", perm)))
	}

	for idx, networkDefinition := range perms {
		ctx := log.WithCtx(ctx, z.Int("index", idx), z.Int("total", len(perms)))

		if err := runNetwork(ctx, networkDefinition, encCtx); err != nil {
			log.Error(ctx, "", err)
			break
		}
	}

	return
}

func runNetwork(ctx context.Context, networkDefinition model.NetworkDefinition, encCtx *enclaves.EnclaveContext) error {
	var ndBytes bytes.Buffer

	if err := kurtosis.GenerateNetworkParams(networkDefinition, &ndBytes); err != nil {
		return errors.Wrap(err, "cannot generate kurtosis network params")
	}

	runCtx, cancel, err := encCtx.RunStarlarkRemotePackage(ctx, "github.com/kurtosis-tech/ethereum-package", &starlark_run_config.StarlarkRunConfig{
		SerializedParams: ndBytes.String(),
	})
	if err != nil {
		return errors.Wrap(err, "cannot run ethereum package")
	}

	defer cancel()

	for element := range runCtx {
		switch v := element.RunResponseLine.(type) {
		case *kurtosis_core_rpc_api_bindings.StarlarkRunResponseLine_Error:
			return errors.New(v.Error.String())
		case *kurtosis_core_rpc_api_bindings.StarlarkRunResponseLine_Warning:
			log.Warn(ctx, "", errors.New(v.Warning.String()))
		case *kurtosis_core_rpc_api_bindings.StarlarkRunResponseLine_ProgressInfo:
			log.Info(ctx, v.ProgressInfo.String())
		}
	}

	return nil
}
