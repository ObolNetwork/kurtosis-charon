# Kurtosis-Charon

## Overview

This project leverages [kurtosis](https://docs.kurtosis.com) to run a local setup of ethereum and beacon chains with Charon DV.
It does not connect to real beacon chains and instead it creates a preconfigured local environment, similar to [Ganache](https://archive.trufflesuite.com/ganache/).
Once started, you will have several instances of execution clients, consensus clients, charons and validator clients running altogether.
What's important to us: kurtosis takes care of configuring and activating validator keys, we only need to import the keys to run the DV.

We need to specify the desired combination of BN and VC.

## Pre-requisites

You need to have docker installed on your machine, because everything is running in docker.

Install `kurtosis-cli` by following the instructions [here](https://docs.kurtosis.com/install).
This project updates frequently, make sure to update it to the latest version before you start.

### Setup - MacOS

On MacOS, use `brew` as following:

```shell
brew install kurtosis-tech/tap/kurtosis-cli
brew install jq
brew install envsubst
brew install bash
```

After installation, verify that this command prints the new version of `bash`:

```shell
/usr/bin/env bash --version # GNU bash, version 5.2.26(1)-release (aarch64-apple-darwin23.2.0)
```

## Usage - local

1. Decide on the desired combination and versions of ELs, CLs, Charon and VCs you need for testing.
You can specify the versions for each of those under `./deployments/env`. The scripts onwards will read from them.

2. Setup monitoring.
By default Prometheus is configured to send data to Obol's GCP service. This, however, requires a monitoring token supplied to you by Obol.
You should export it as an environemnt variable or add it to the `./deployments/env/charon.env` as `PROMETHEUS_REMOTE_WRITE_TOKEN`.
If you do not have token and/or do not want to supply the metrics from the run to Obol, you can set `EXTERNAL_MONITORING` to false in `./deployments/env/charon.env`.

3. Run combination.

```shell
make geth-<CL>-charon-<VC>
```

i.e.:

```shell
make geth-lighthouse-charon-lodestar
```

4. Shutting down.

```shell
make clean
```

This command will stop everything in docker and properly clean up the environment. Charon cluster files and all kurtosis intermediate files will be deleted.
In docker you will notice three kurtosis containers running - that's normal, they are used to manage the setup. It's safe to stop/kill them, because kurtosis will recreate them each time you run any kurtosis command.

## Usage - Docker

> [!WARNING]
> Still in its alpha stages, so your mileage may vary.

There is an option to also run the whole stack from a docker container, utilising Docker's official docker-in-docker setup.

1. Build the Docker image.

> [!Note]
> Soon we will supply that from obolnetwork's dockerhub registry.

```shell
docker build . -t obolnetwork/kurtosis-charon
```

2. Decide on the desired combination and versions of ELs, CLs, Charon and VCs you need for testing.
You can specify the clients and their versions by copying the `.env.sample` into `.env` file and fill it out.

3. Setup monitoring.
By default Prometheus is configured to send data to Obol's GCP service. This, however, requires a monitoring token supplied to you by Obol.
You should add it to the `.env`.
If you do not have token and/or do not want to supply the metrics from the run to Obol, you can set `EXTERNAL_MONITORING` to false. Note that as everything will run into one single container, you'll need to port forward Grafana's 3000 port.

4. Run the combination.

> [!Note]
> Running docker in priviliged is required. Unfortunately, there is no way around it when running docker-in-docker. Read more [here](https://www.docker.com/blog/docker-can-now-run-within-docker/). If this raises any concerns to you, we advise on running it the regular way, as seen under [Usage - local](#usage---local).

```shell
docker run --privileged --env-file=.env obolnetwork/kurtosis-charon
```

5. Shutting down.
Simply stop the container.

## Notes

### Validators

We configured this project to run 256 validators per node (each "node" is EL+CL+DV). Therefore, for 3 nodes, you will have 768 validators in total in this beacon chain. This large amount is necessary to fulfill beachon chain requirements about committees. For us it is good to test DV with that large number of validators, therefore do not change this.

Out of the original combination of 3xELs + 3xCLs + 3xVCs one VC (that serves 256 validators) is stopped and a charon DV is started in its place with 3xCharon nodes and 3xVCs that talk to the Charon nodes. Meaning the final combination that is run is the following:

3x ELs
3x CLs
2x VCs (traditional)

3x Charons
3x VCs (serving the DV)

With again, total 768 validators.

### System resources

A typical stack that runs everything in docker occupies 3-4 GB of RAM in according with docker stats (on MacOS, involving Rosetta in some cases) and moderate CPU usage.

### Nimbus BN

Due to an issue in Nimbus (probably applicable to kurtosis only), when running as a beacon node it requires clients to use JSON request format, not SSZ. In Charon we added "json_requests" feature that must be enabled if you choose running Nimbus as BN. Before running Charon, update `docker-compose.yml` to include this argument: `--feature-set-enable=json_requests`.

### Failed initial duties

At the beginning of the run, you will notise after ~1 epoch that Charon's inclusion checker reports missed duties. This is expected, as those are the duties missed between stopping the traditional VC and starting the Charon cluster.

### Acceptance Criteria

Once you have the full stack up and running, you will be watching for logs produced by charon nodes and VC instance in docker. Also, you will be using the mentioned Grafana dashboard to see all the metrics pushed by your cluster.

* Charon logs do not contain any critical errors. Consensus rounds must succeed. All calls from VC and to BN are fulfilled with no errors.
* VC instances logs do not contain any critical errors.
* In Grafana watch for the well-known health conditions, such as progressing duties, BN errors, VC errors, consensus rounds, timeouts, etc.
* Make sure DV produces blocks and proposer duty is successful.

If, after at least 4 epochs you do not observe any critical events, you can consider the DV is executed *successfully*.
