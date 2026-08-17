#!/usr/bin/env bash

# Terminate the native-Charon AWS test fleet (all 36 combo instances).

set -euo pipefail

python3 -m venv ./aws
source ./aws/bin/activate
trap deactivate EXIT
pip3 install -r aws/requirements.txt -q

python3 aws/kurtosis_aws_runner.py --terminate
