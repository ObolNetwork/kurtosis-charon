# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Kurtosis-Charon is a test harness for running Ethereum networks with Charon Distributed Validator (DV) clusters. It uses ObolNetwork's fork of ethereum-package (the `obol` branch, tagged `6.1.0-obol`) with Kurtosis to spin up full EL+CL+Charon+VC stacks in a single `kurtosis run` command.

All 36 CL x VC combos (6 CLs: lighthouse, lodestar, nimbus, teku, prysm, grandine; 6 VCs: lighthouse, lodestar, nimbus, teku, prysm, vouch) are defined as self-contained args-files in `deployments/<cl>-<vc>.yaml`.

## Common Commands

### Running a local cluster
```bash
make start-local/lighthouse-lodestar
make stop-local/lighthouse-lodestar
make status-local
make clean
```

### AWS fleet testing
```bash
export CHARON_VERSION=v1.11.0
export PROMETHEUS_REMOTE_WRITE_TOKEN=<token>
make start-aws                           # all 36 combos
make stop-aws
make status-aws                          # terminate fleet

# Filter combos:
ONLY=cl:lighthouse make start-aws        # 6 combos with lighthouse CL
ONLY=lighthouse-vouch make start-aws     # single combo
```

## Architecture

### Network Params

`deployments/*.yaml` are self-contained Kurtosis args-files. They use `$CHARON_VERSION` as a placeholder for the Charon Docker image tag, substituted at runtime by:
- **DV Runner**: `strings.ReplaceAll` in Go (defaults to `next`)
- **AWS Runner**: `envsubst` in cloud-init (pinned stable release via `--charon-version`)

### AWS Runner

`aws/kurtosis_aws_runner.py` launches one EC2 instance per combo. Charon version is a required `--charon-version` CLI arg. The `--only` flag supports `cl:<client>`, `vc:<client>`, and exact combo names with union semantics.

### DV Runner

`local/runner/` is a Go program running 24/7, cycling all 36 combos sequentially. Uses `charon:next` by default (override with `RUNNER_CHARON_TAG` env var). After each run it scores duty success rates from Prometheus, reports to Slack, and archives logs on failure.

## Prerequisites

- Docker
- `kurtosis-cli` 1.20.0+
- Python 3.8+ with `boto3` (AWS only)
