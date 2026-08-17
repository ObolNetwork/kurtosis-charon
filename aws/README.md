# Kurtosis AWS Runner

Launches a fleet of EC2 instances, each running a different CL x VC combo with Charon via native Kurtosis. All 36 combos (6 CLs x 6 VCs) are supported, one instance per combo.

## Requirements

- Python 3.8+
- AWS CLI configured with access to start EC2 instances

## Setup

```bash
cd kurtosis-charon/aws
python3 -m venv venv
source venv/bin/activate
pip install -r requirements.txt
```

## Usage

The simplest way to launch is via the Makefile wrapper:

```bash
export CHARON_VERSION=v1.11.0
export PROMETHEUS_REMOTE_WRITE_TOKEN=<token>
make run-aws
```

Or call the runner directly:

```bash
python3 kurtosis_aws_runner.py \
  --monitoring-token "$PROMETHEUS_REMOTE_WRITE_TOKEN" \
  --charon-version v1.11.0 \
  --on-demand \
  --branch main
```

### Filtering combos

Use `--only` to run a subset of combos:

```bash
# Single combo
--only lighthouse-vouch

# All combos with a specific CL
--only cl:lighthouse

# All combos with a specific VC
--only vc:vouch

# Multiple filters (union, deduplicated)
--only cl:lighthouse,vc:vouch
```

### Terminating the fleet

```bash
make stop-aws
# or directly:
python3 kurtosis_aws_runner.py --terminate
```

## Configuration

| Flag | Default | Description |
|------|---------|-------------|
| `--monitoring-token` | required | Obol Prometheus remote write token |
| `--charon-version` | required | Charon image tag (e.g. `v1.11.0`) |
| `--branch` | `main` | Git branch to clone on each instance |
| `--lifetime` | `60m` | Auto-shutdown timer per instance |
| `--instance-type` | `c6a.8xlarge` | EC2 instance type |
| `--on-demand` | off | Use On-Demand instead of Spot |
| `--only` | all 36 | Filter combos (see above) |
| `--terminate` | - | Terminate running fleet instances |
