#!/usr/bin/env bash

# Show status of running AWS fleet instances.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
python3 -m venv "$SCRIPT_DIR/.venv"
source "$SCRIPT_DIR/.venv/bin/activate"
trap deactivate EXIT
pip3 install --upgrade pip -q
pip3 install -r "$SCRIPT_DIR/requirements.txt" -q

python3 "$SCRIPT_DIR/kurtosis_aws_runner.py" --status
