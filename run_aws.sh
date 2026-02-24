#!/usr/bin/env bash

# This script is used for AWS deployments.

# Error if EXTERNAL_MONITORING is true and PROMETHEUS_REMOTE_WRITE_TOKEN is not set.
if [ -z "$PROMETHEUS_REMOTE_WRITE_TOKEN" ]; then
  echo "PROMETHEUS_REMOTE_WRITE_TOKEN environment variable is not set for external monitoring."
  exit 1
fi

echo "Logging to AWS SSO..."
SSO_ACCOUNT=$(aws sts get-caller-identity --query "Account")
if [ ${#SSO_ACCOUNT} -eq 14 ];  then 
  echo "AWS SSO already logged in" ;
else 
  aws sso login
  echo "AWS SSO logged in" ;
fi

python3 -m venv ./kurtosis-aws-runner

source ./kurtosis-aws-runner/bin/activate
trap deactivate EXIT

pip3 install -r kurtosis-aws-runner/requirements.txt -q
python3 kurtosis-aws-runner/kurtosis_aws_runner.py --monitoring-token "$PROMETHEUS_REMOTE_WRITE_TOKEN" --env-dir=./deployments/env --on-demand --branch="$(git branch --show-current)" --instance-type=c6a.4xlarge
