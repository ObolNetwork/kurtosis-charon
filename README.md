# Kurtosis-Charon

Test harness for running Ethereum networks with [Charon](https://github.com/ObolNetwork/charon) Distributed Validator clusters, powered by [Kurtosis](https://docs.kurtosis.com) and ObolNetwork's [ethereum-package](https://github.com/ObolNetwork/ethereum-package/tree/obol) fork.

A single `kurtosis run` command spins up the full stack: EL + CL + Charon DV middleware + VC. All 36 CL x VC combinations (6 CLs x 6 VCs) are defined as self-contained args-files in `deployments/`.

## Quick Start

### Prerequisites

- Docker
- [kurtosis-cli](https://docs.kurtosis.com/install) 1.20.0+

### Run a local cluster

```bash
make run-local/lighthouse-lodestar
```

Watch the Kurtosis enclave come up, then inspect with `kurtosis enclave inspect lighthouse-lodestar`.

### Cleanup

```bash
make stop-local/lighthouse-lodestar
# or clean everything:
make clean
```

## AWS Fleet Testing

Launch all 36 combos on EC2 (one instance per combo):

```bash
export CHARON_VERSION=v1.11.0
export PROMETHEUS_REMOTE_WRITE_TOKEN=<your-token>
make run-aws
```

Filter with `--only`:

```bash
# All combos with lighthouse CL (6 combos)
ONLY=cl:lighthouse make run-aws

# Specific combo
ONLY=lighthouse-vouch make run-aws

# Multiple filters (union)
ONLY=cl:lighthouse,vc:vouch make run-aws
```

Terminate the fleet:

```bash
make stop-aws
```

See `aws/README.md` for full details.

## DV Runner

The DV runner (`local/runner/`) runs 24/7, cycling all 36 combos and reporting results to Slack. It uses `charon:next` by default for bleeding-edge testing. See `local/runner/README.md` for configuration.

## Network Params

Args-files live in `deployments/<cl>-<vc>.yaml`. They use `$CHARON_VERSION` as a placeholder, substituted at runtime by the runner and AWS runner. Client versions are pinned directly in each YAML file.

## Notes

### Validators

Each combo runs 256 validators per node (768 total across 3 nodes). One of the 3 traditional VCs is replaced by a 3-node Charon DV cluster with 3 DVT-aware VCs.

### Nimbus CL

When running Nimbus as a consensus client, Charon must use JSON request format instead of SSZ. The args-files for Nimbus CL combos include `CHARON_FEATURE_SET_ENABLE: json_requests` automatically.

### Failed Initial Duties

Missed duties in the first epoch are expected - they correspond to the transition period between the traditional VC being replaced and the Charon DV cluster starting.
