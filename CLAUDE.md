# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Kurtosis-Charon is a test harness for running local Ethereum networks with Charon Distributed Validator (DV) clusters. It uses Kurtosis (`ethereum-package` from ethpandaops) to spin up Execution Layer (EL) and Consensus Layer (CL) clients, then layers Charon DV middleware and Validator Clients (VC) on top via Docker Compose.

## Common Commands

### Running a local cluster (Make targets)

Targets follow the pattern `{el}-{cl}-charon-{vc}`:
```bash
make geth-lighthouse-charon-lodestar   # Most common combo (also used in CI)
make geth-nimbus-charon-lighthouse
make geth-teku-charon-teku
```

Each target chains: `setup_el_cl.sh` → `setup_charon.sh` → `setup_monitoring.sh` → `setup_vc.sh` → `docker compose up`.

### Cleanup
```bash
make clean   # Stops containers, removes .charon/ and data/, runs kurtosis clean
```

### AWS fleet testing
```bash
make run-aws    # Launches EC2 instances for all client combos (requires AWS SSO)
make stop-aws   # Terminates running AWS instances
```

### Voluntary exits
```bash
make exit-lighthouse   # Also: exit-nimbus, exit-lodestar, exit-teku
```

## Architecture

### Execution Flow

1. **`setup_el_cl.sh`** — Loads env vars from `deployments/env/`, substitutes versions into network params YAML, runs `kurtosis run` with `ethereum-package` to deploy EL+CL nodes
2. **`setup_charon.sh`** — Extracts validator keys from Kurtosis enclave, creates a 3-node Charon cluster (`charon create cluster`), kills the first traditional VC that Kurtosis started
3. **`setup_monitoring.sh`** — Configures Prometheus (optionally with Obol remote_write)
4. **`setup_vc.sh`** — Prepares VC data directories and loads VC-specific env vars
5. **Docker Compose** — Brings up Charon nodes + VCs + monitoring stack

### Docker Compose Files

- `compose.charon.yaml` — 3 Charon nodes + Prometheus + Grafana (core, always used)
- `compose.{lighthouse,lodestar,nimbus,prysm,teku,vouch}.yaml` — 3 VC instances per client type
- Services connect to a Kurtosis-created external Docker network (`${NETWORK_NAME}`)

### Configuration

- **`deployments/env/`** — Per-client env files (`el_*.env`, `cl_*.env`, `vc_*.env`, `charon.env`) controlling image versions
- **`deployments/network_params/`** — Kurtosis network param YAML files; `network_params_base.yaml` has shared config, client-specific files override CL settings
- **`.env.sample`** — Template for local `.env` file (EL_TYPE, CL_TYPE, VC_TYPE, CHARON_VERSION, EXTERNAL_MONITORING)

### Testnet Parameters

- Chain ID: 3151908, slot time: 12s, 256 validators/node (768 total across 3 nodes)
- Fork version: 0x10000038, Capella: 0x40000038
- Deposit contract: 0x4242424242424242424242424242424242424242

### Validator Client Run Scripts

Each VC directory (`lighthouse/`, `lodestar/`, `prysm/`, `nimbus/`, `teku/`, `vouch/`) contains a `run.sh` that imports BLS keystores and starts the client with appropriate flags for Charon's validator API.

### Monitoring Stack

- **Prometheus** (port 9090) — Scrapes Charon nodes (port 3620) and VCs
- **Grafana** (port 3000) — Dashboard: `dash_charon_overview.json`
- **Tempo + OTel Collector** — Distributed tracing

### AWS Runner

`kurtosis-aws-runner/kurtosis_aws_runner.py` — Python script that discovers all client combos from `deployments/env/`, launches separate EC2 Spot instances per combo, injects monitoring tokens, and auto-terminates after configurable lifetime.

## CI

GitHub Actions workflow (`.github/workflows/run-cluster.yaml`) runs `make geth-lighthouse-charon-lighthouse` on push to main (when `templates/**` changes) or manual dispatch with a SHA input.

## Prerequisites

- Docker & Docker Compose
- Bash 5.2+
- `kurtosis-cli` 1.7.2+
- `jq`, `gettext` (for envsubst)
- Python 3.8+ with `boto3` (AWS deployments only)

## Test Success Criteria

A healthy cluster run (minimum 4 epochs / ~50 min) should show:
- No critical errors in Charon logs
- All consensus rounds succeed
- No BN/VC errors in Grafana
- Block production and proposer duties succeed
- Failed duties in first ~1 epoch are expected (VC transition period)
