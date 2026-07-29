#!/usr/bin/env bash

# Terminate the native-Charon AWS test fleet (all 36 combo instances).

set -euo pipefail

python3 -m venv ./kurtosis-aws-runner
source ./kurtosis-aws-runner/bin/activate
trap deactivate EXIT
pip3 install -r kurtosis-aws-runner/requirements.txt -q

python3 kurtosis-aws-runner/kurtosis_aws_runner_native.py --terminate
