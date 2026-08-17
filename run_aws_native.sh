#!/usr/bin/env bash

# Launch the native-Charon Kurtosis test fleet on AWS: 6 CL x 6 VC = 36 combos,
# one EC2 instance per combo, all on c6a.4xlarge (uniform), On-Demand,
# self-terminating after --lifetime. Charon is pinned to an exact stable version
# in the deployment args-files under deployments/network_params/.
#
# The runner clones this repo at the CURRENT branch, so the branch must be PUSHED
# to origin before launching. Override lifetime with LIFETIME (default 60m).

set -euo pipefail

if [ -z "${PROMETHEUS_REMOTE_WRITE_TOKEN:-}" ]; then
  echo "PROMETHEUS_REMOTE_WRITE_TOKEN environment variable is not set for external monitoring."
  exit 1
fi

if [ -z "${CHARON_VERSION:-}" ]; then
  echo "CHARON_VERSION environment variable is not set (e.g. export CHARON_VERSION=v1.11.0)."
  exit 1
fi

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

echo "Logging to AWS SSO..."
SSO_ACCOUNT=$(aws sts get-caller-identity --query "Account")
if [ ${#SSO_ACCOUNT} -eq 14 ]; then
  echo "AWS SSO already logged in"
else
  aws sso login
  echo "AWS SSO logged in"
fi

python3 -m venv ./kurtosis-aws-runner
source ./kurtosis-aws-runner/bin/activate
trap deactivate EXIT
pip3 install -r kurtosis-aws-runner/requirements.txt -q

python3 kurtosis-aws-runner/kurtosis_aws_runner_native.py \
  --monitoring-token "$PROMETHEUS_REMOTE_WRITE_TOKEN" \
  --charon-version "$CHARON_VERSION" \
  --on-demand \
  --branch="$BRANCH" \
  --instance-type=c6a.4xlarge \
  --lifetime="$LIFETIME"
