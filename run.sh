#!/usr/bin/env bash

if ! [ -f .env ]; then
  echo ".env does not exist, using supplied env variables."
else
  echo "Loading .env file."
  export $(xargs <.env)
fi

./setup_el_cl.sh

kurtosis run --enclave local-eth-testnet github.com/ethpandaops/ethereum-package --args-file ./network_params.yaml >planprint
echo "Waiting for 10 seconds..."
sleep 10

./setup_charon.sh
./setup_monitoring.sh
./setup_vc.sh

# Reload .env as new variables from previous commands are added to it
export $(xargs <.env)

docker compose \
  --env-file .env \
  -f ./compose.charon.yaml \
  -f ./compose.${VC_TYPE}.yaml \
  up -d --quiet-pull
