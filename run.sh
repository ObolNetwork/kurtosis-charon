#!/usr/bin/env bash

# This script is used mainly by Docker. For local runs, stick to the Makefile.

# Load .env if it exists
if ! [ -f .env ]; then
  echo ".env does not exist, using supplied env variables."
else
  echo "Loading .env file."
  export $(xargs <.env)
fi

# Setup the Kurtosis EL and CL.
./setup_el_cl.sh

kurtosis run --enclave local-eth-testnet github.com/ethpandaops/ethereum-package --args-file ./network_params.yaml >planprint
echo "Waiting for 10 seconds..."
sleep 10

# Setup Charon, Monitoring and Charon VCs.
./setup_charon.sh
./setup_monitoring.sh
./setup_vc.sh

# Add env variables for Docker containers. Required in Linux.
if ! grep -q DUID ./.env; then
  echo "DUID=$(id -u)" >>./.env
fi

if ! grep -q DGID ./.env; then
  echo "DGID=$(id -g)" >>./.env
fi

# Reload .env as new variables from previous commands are added to it.
export $(xargs <.env)

# Start the test.
docker compose \
  --env-file .env \
  -f ./compose.charon.yaml \
  -f ./compose.${VC_TYPE}.yaml \
  up -d --quiet-pull
