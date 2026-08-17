#!/usr/bin/env bash

# Launch the native-Charon Kurtosis test fleet on AWS: all 36 CL x VC combos,
# one EC2 instance per combo, all on c6a.4xlarge (uniform), On-Demand,
# self-terminating after --lifetime. Charon version is set via CHARON_VERSION.
#
# The runner clones this repo at the CURRENT branch, so the branch must be PUSHED
# to origin before launching. Override lifetime with LIFETIME (default 60m).

set -euo pipefail

if [ -z "${PROMETHEUS_REMOTE_WRITE_TOKEN:-}" ]; then
  echo "PROMETHEUS_REMOTE_WRITE_TOKEN environment variable is not set for external monitoring."
  exit 1
fi

CHARON_VERSION="${CHARON_VERSION:-v1.10.3}"

BRANCH="$(git branch --show-current)"
LIFETIME="${LIFETIME:-60m}"

# Warn if the current branch isn't pushed / is behind origin (EC2 clones from origin).
if git rev-parse --verify --quiet "origin/${BRANCH}" >/dev/null; then
  if [ -n "$(git rev-list "origin/${BRANCH}..HEAD" 2>/dev/null)" ]; then
    echo "⚠️  Local '${BRANCH}' has commits not on origin. Push before launching (EC2 clones origin/${BRANCH})."
    exit 1
  fi
else
  echo "⚠️  origin/${BRANCH} does not exist. Push the branch first: git push -u origin ${BRANCH}"
  exit 1
fi

if aws sts get-caller-identity --output text >/dev/null 2>&1; then
  echo "AWS SSO already logged in"
else
  echo "Logging to AWS SSO..."
  aws sso login
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
python3 -m venv "$SCRIPT_DIR/.venv"
source "$SCRIPT_DIR/.venv/bin/activate"
trap deactivate EXIT
pip install --upgrade pip -q
pip3 install -r "$SCRIPT_DIR/requirements.txt" -q

RUNNER_ARGS=(
  --monitoring-token "$PROMETHEUS_REMOTE_WRITE_TOKEN"
  --charon-version "$CHARON_VERSION"
  --on-demand
  --branch="$BRANCH"
  --instance-type=c6a.4xlarge
  --lifetime="$LIFETIME"
)

if [ -n "${ONLY:-}" ]; then
  RUNNER_ARGS+=(--only "$ONLY")
fi

python3 "$SCRIPT_DIR/kurtosis_aws_runner.py" "${RUNNER_ARGS[@]}"
