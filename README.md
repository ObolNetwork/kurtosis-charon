# Kurtosis-Charon

## Overview

This project leverages [kurtosis](https://docs.kurtosis.com) to run a local setup of ethereum and beacon chains with DV.
It does not connect to real beacon chains and instead it creates a preconfigured local environment, similar to [Ganache](https://archive.trufflesuite.com/ganache/).
Once started, you will have several instances of execution clients, consensus clients, charon and validator clients running altogether.
What's important to us: kurtosis takes care of configuring and activating validator keys, we only need to import the keys to run DV.

Kurtosis supports all of the existing vendors: lighthouse, nimbus, prysm, teku and lodestar for both BN and VC roles.
We only need to specify the desired combination of BN and VC. In all cases it uses `geth` for EL.
Note that at the time of writing, not every combination of BN and VC [is supported](https://github.com/ethpandaops/ethereum-package?tab=readme-ov-file#beacon-node--validator-client-compatibility).

## Pre-requisites

You need to have docker installed on your machine, because everything is running in docker.

Install `kurtosis-cli` by following the instructions [here](https://docs.kurtosis.com/install).
This project updates frequently, make sure to update it to the latest version before you start.

### MacOS

On MacOS, use `brew` as following:

```shell
brew install kurtosis-tech/tap/kurtosis-cli
brew install jq
brew install bash
```

> Note: `jq` is used in the scripts to parse JSON files, and for `bash` this installs the latest version (5.x+) required by scripts.

After installation, verify that this command prints the new version of `bash`:

```shell
/usr/bin/env bash --version # GNU bash, version 5.2.26(1)-release (aarch64-apple-darwin23.2.0)
```

## Usage

1. Start the docker.

2. Build the charon image from your local charon repository: `docker build . -t obolnetwork/charon-local`.

3. If you want to use the specific charon release from Docker Hub:

```shell
docker image pull obolnetwork/charon:v1.1-dev
docker tag obolnetwork/charon:v1.1-dev obolnetwork/charon-local:latest
```
> The script in this project expects charon image to have `obolnetwork/charon-local:latest` tag.

4. Decide on the desired combination of BN and VC you need for testing. Start kurtosis with EL+CL stack by running:

```shell
# Pick one of these commands
make geth-lighthouse
make geth-lodestar
make geth-teku
make geth-nimbus
make geth-prysm
```

> Note: this command often fails if you have leftovers from the previous run. In that case, run `make clean` and try again.
Also, sometimes docker network-related errors can only be fixed with a complete docker daemon restart.

After executing this, in docker you will see bunch of containers running (assuming Teku is chosen):
* EL containers: el-1-geth-teku...el-3-geth-teku
* CL containers: cl-1-teku-geth...cl-3-teku-geth
* VC containers: vc-1-teku-geth...vc-3-teku-geth

By default it is running 3 instances of each type. You can change the number of instances: `count: 3` in .yaml files.
Now you have a local setup of ethereum and beacon chains running and progressing with normal (non-DV) validators.

5. Use your prometheus write token and export `PROMETHEUS_REMOTE_WRITE_TOKEN`.

This will make your DV pushing metrics (not logs) to Obol Labs' Grafana.

6. Create charon solo cluster configuration by running:

```shell
make charon
```

This script will create a solo cluster using validator keys pulled from the first VC instance `vc-1-teku-geth`.
It will also create the `.env` file with the necessary environment variables for the future docker compose run.
Because it takes the keys from the first VC instance, at the end of the script it kills that instance to prevent conflicts.
Now you are ready to run the DV.

7. Run the DV by running:

```shell
# Pick one of these commands
make run-charon-lighthouse
make run-charon-nimbus
make run-charon-teku
make run-charon-prysm
make run-charon-lodestar
```

This time, it runs `docker-compose.yml` with the charon configuration and the selected validator clients.
Each charon instance will connect to its own BN instance (see `BN_0`...`BN_2` exported in `.env` file). This assures high BN score.
Also, each charon instance will have its own VC instance of the selected type (vendor).
This way we create a complete solo DV cluster.

8. Monitoring

If everything runs fine, you will see the DV running and pushing metrics to the Grafana:
https://grafana.monitoring.gcp.obol.tech/d/b962e704-2e37-48a4-82c0-b15d7661e8a6/charon-overview-v3-testnet-updates?orgId=1&var-cluster_network=testnet

> Note that this is the special dashboard designed to monitor "testnet", don't forget to switch to this "Cluster Network". Then select the "Cluster Hash" matching your solo cluster hash found in `.charon/cluster/node0/cluster-lock.json`.

Allow at least one epoch to pass before you make any conclusions. See *Notes* below for the *Definition of Success*.

9. Shutting down

```shell
make clean
```

This command will stop everything in docker and properly clean up the environment. Charon cluster files and all kurtosis intermediate files will be deleted.
In docker you will notice three kurtosis containers running - that's normal, they are used to manage the setup. It's safe to stop/kill them, because kurtosis will recreate them each time you run any kurtosis command.

## Notes

### Validators

We configured this project to run 600 validators per node (each "node" is EL+CL+DV). Therefore, for 3 nodes, you will have 1800 validators in total in this beacon chain. This large amount is necessary to fulfill beachon chain requirements about committees. For us it is good to test DV with that large number of validators, therefore do not change this.

### The Long Delay

When you run `make geth-teku` or other commands (step 4 in above), you will notice the script waits for N seconds after the stack seems booted. This is because the beacon chain needs some time to sync and start producing blocks. The delay is set to 10-60 seconds depending on the stack. Do not skip this delay.

### System resources

A typical stack that runs everything in docker occupies 3-4 GB of RAM in according with docker stats (on MacOS, involving Rosetta in some cases) and moderate CPU usage.

### Prysm 5.0.3

Prysm 5.0.3 has some known issues:
* The known bug that is [fixed](https://github.com/prysmaticlabs/prysm/pull/13995) and will be released in the next version of Prysm. Until then, you will see some duties failing,
unless you create a custom build of Prysm with this fix: `pinebit/prysm-vc:latest`. This only applies to VC.
* The known issues with memory manangement, we experienced OOM kills of Prysm BN instances soon after the start. This issue is already reported in their backlog.

### Nimbus BN

Due to an issue in Nimbus (probably applicable to kurtosis only), when running as a beacon node it requires clients to use JSON request format, not SSZ. In Charon we added "json_requests" feature that must be enabled if you choose running Nimbus as BN. Before running Charon, update `docker-compose.yml` to include this argument: `--feature-set-enable=json_requests`.

### Acceptance Criteria

Once you have the full stack up and running, you will be watching for logs produced by charon nodes and VC instance in docker. Also, you will be using the mentioned Grafana dashboard to see all the metrics pushed by your cluster.

* Charon logs do not contain any critical errors. Consensus rounds must succeed. All calls from VC and to BN are fulfilled with no errors.
* VC instances logs do not contain any critical errors.
* In Grafana watch for the well-known health conditions, such as progressing Duties, BN errors, VC errors, consensus rounds, timeouts, etc.
* Make sure DV produces blocks and proposer duty is successful.

If, after at least 15 minutes you do not observe any critical events, you can consider the DV is executed *successfully*.
