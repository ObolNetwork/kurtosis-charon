#!/usr/bin/env bash

apt-get update && apt-get install -y curl jq wget

while ! curl "${LIGHTHOUSE_BEACON_NODE_ADDRESS}/eth/v1/node/health" 2>/dev/null; do
  echo "Waiting for ${LIGHTHOUSE_BEACON_NODE_ADDRESS} to become available..."
  sleep 5
done

# Refer: https://lighthouse-book.sigmaprime.io/advanced-datadir.html
# Running a lighthouse VC involves two steps which needs to run in order:
# 1. Loading the validator keys
# 2. Actually running the VC

for f in /opt/charon/keys/keystore-*.json; do
  echo "Importing key ${f}"
  lighthouse account validator import \
    --reuse-password \
    --keystore "${f}" \
    --password-file "${f//json/txt}" \
    --testnet-dir "/opt/lighthouse/network-configs"
done

echo "Starting lighthouse validator client for ${NODE}"
exec lighthouse validator \
  --beacon-nodes ${LIGHTHOUSE_BEACON_NODE_ADDRESS} \
  --suggested-fee-recipient "0x0000000000000000000000000000000000000000" \
  --metrics \
  --metrics-address "0.0.0.0" \
  --metrics-allow-origin "*" \
  --metrics-port "5064" \
  --use-long-timeouts \
  --testnet-dir "/opt/lighthouse/network-configs" \
  --builder-proposals \
  --distributed \
  --debug-level "debug"
