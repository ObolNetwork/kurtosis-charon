package kurtosis

import (
	"context"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	docker "github.com/docker/docker/client"
	"github.com/kurtosis-tech/kurtosis/api/golang/core/lib/enclaves"
	kcontext "github.com/kurtosis-tech/kurtosis/api/golang/engine/lib/kurtosis_context"
	"github.com/obolnetwork/charon/app/errors"
	"github.com/obolnetwork/charon/app/z"
	"os/exec"
)

func connectDocker() (*docker.Client, error) {
	cli, err := docker.NewClientWithOpts(docker.FromEnv)
	if err != nil {
		return nil, errors.Wrap(err, "can't reach docker")
	}

	return cli, nil
}

// Stop stops all the Kurtosis-related containers and deletes them from the Docker storage,
// including the associated volumes.
func Stop(ctx context.Context) error {
	if kctx, err := connectKurtosis(); err == nil { // best-effort cleanup
		if err := kctx.StopEnclave(ctx, "local-eth-testnet"); err != nil {
			return errors.Wrap(err, "cannot stop kurtosis enclave")
		}

		if err := kctx.DestroyEnclave(ctx, "local-eth-testnet"); err != nil {
			return errors.Wrap(err, "cannot destroy kurtosis enclave")
		}

		if _, err := kctx.Clean(ctx, true); err != nil {
			return errors.Wrap(err, "cannot clean kurtosis")
		}
	}

	return ClearContainers(ctx)
}

func ClearContainers(ctx context.Context) error {
	cli, err := connectDocker()
	if err != nil {
		return err
	}

	list, err := cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("name", "kurtosis*")),
	})
	if err != nil {
		return errors.Wrap(err, "cannot list containers")
	}

	for _, li := range list {
		if err := cli.ContainerRemove(ctx, li.ID, container.RemoveOptions{
			Force:         true,
			RemoveVolumes: true,
		}); err != nil {
			return errors.Wrap(err, "cannot remove container", z.Str("id", li.ID))
		}
	}

	if err := cli.NetworkRemove(ctx, "kt-local-eth-testnet"); err != nil {
		if !docker.IsErrNotFound(err) {
			return errors.Wrap(err, "cannot remove Kurtosis network container", z.Str("name", "kt-local-eth-testnet"))
		}
	}

	return nil
}

func Start(ctx context.Context) (*kcontext.KurtosisContext, *enclaves.EnclaveContext, error) {
	cmd := exec.Command("kurtosis", "engine", "start")
	if err := cmd.Run(); err != nil {
		return nil, nil, errors.Wrap(err, "kurtosis start error")
	}

	kctx, err := connectKurtosis()
	if err != nil {
		return nil, nil, errors.Wrap(err, "cannot connect to kurtosis local engine")
	}

	encCtx, err := kctx.CreateEnclave(ctx, "local-eth-testnet")
	if err != nil {
		return nil, nil, errors.Wrap(err, "cannot create enclave")
	}

	return kctx, encCtx, nil
}

func connectKurtosis() (*kcontext.KurtosisContext, error) {
	kctx, err := kcontext.NewKurtosisContextFromLocalEngine()
	if err != nil {
		return nil, err
	}

	return kctx, nil
}
