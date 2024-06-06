package main

import (
	"context"
	"fmt"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	docker "github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/obolnetwork/charon/app/errors"
	"github.com/obolnetwork/charon/app/z"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

const (
	testbedPromContainerName = "kurtosis-testbed-prometheus"
)

func startLocalPrometheus(ctx context.Context) error {
	cli, err := connectDocker()
	if err != nil {
		return errors.Wrap(err, "docker error")
	}

	// make sure we're starting with a clean prometheus setup
	if err := deleteLocalPrometheus(ctx, cli); err != nil && !docker.IsErrNotFound(err) {
		return errors.Wrap(err, "cannot delete testbed prometheus")
	}

	writePort, err := nat.NewPort("tcp", "9090")
	if err != nil {
		return errors.Wrap(err, "can't create port binding object")
	}

	resp, err := cli.ContainerCreate(ctx, &container.Config{
		ExposedPorts: map[nat.Port]struct{}{
			writePort: {},
		},
		Image: "prom/prometheus:latest",
	}, &container.HostConfig{
		PortBindings: map[nat.Port][]nat.PortBinding{
			writePort: {
				nat.PortBinding{
					HostIP:   "0.0.0.0",
					HostPort: "9090",
				},
			},
		},
	}, &network.NetworkingConfig{}, &ocispec.Platform{}, testbedPromContainerName)
	if err != nil {
		return errors.Wrap(err, "cannot create container", z.Str("container_name", testbedPromContainerName))
	}

	if len(resp.Warnings) != 0 {
		return errors.New("docker warnings", z.Str("warnings", fmt.Sprintf("%+v", resp.Warnings)), z.Str("container_name", testbedPromContainerName))
	}

	err = cli.ContainerStart(ctx, testbedPromContainerName, container.StartOptions{})
	if err != nil {
		return errors.Wrap(err, "cannot start prometheus container", z.Str("container_name", testbedPromContainerName))
	}

	return nil
}

func stopLocalPrometheus(ctx context.Context) error {
	cli, err := connectDocker()
	if err != nil {
		return errors.Wrap(err, "docker error")
	}

	if err := cli.ContainerStop(ctx, testbedPromContainerName, container.StopOptions{}); err != nil {
		return errors.Wrap(err, "cannot stop container", z.Str("container_name", testbedPromContainerName))
	}

	if err := deleteLocalPrometheus(ctx, cli); err != nil && !docker.IsErrNotFound(err) {
		return errors.Wrap(err, "cannot delete testbed prometheus")
	}

	return nil
}

func deleteLocalPrometheus(ctx context.Context, cli *docker.Client) error {
	if err := cli.ContainerRemove(ctx, testbedPromContainerName, container.RemoveOptions{
		RemoveVolumes: true,
		Force:         false,
	}); err != nil {
		return errors.Wrap(err, "cannot remove container", z.Str("container_name", testbedPromContainerName))
	}

	return nil
}

func connectDocker() (*docker.Client, error) {
	cli, err := docker.NewClientWithOpts(docker.FromEnv)
	if err != nil {
		return nil, errors.Wrap(err, "can't reach docker")
	}

	return cli, nil
}
