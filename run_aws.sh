#!/usr/bin/env bash

# This script is used for AWS deployments.

# Error if EXTERNAL_MONITORING is true and PROMETHEUS_REMOTE_WRITE_TOKEN is not set.
if [ -z "$PROMETHEUS_REMOTE_WRITE_TOKEN" ]; then
  echo "PROMETHEUS_REMOTE_WRITE_TOKEN environment variable is not set for external monitoring."
  exit 1
fi

	python3 kurtosis-aws-runner/kurtosis_aws_runner.py --monitoring-token "$PROMETHEUS_REMOTE_WRITE_TOKEN" --env-dir=./deployments/env --on-demand --branch="$(git branch --show-current)"
