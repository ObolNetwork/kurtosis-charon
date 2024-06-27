#!/usr/bin/env bash

docker exec -it kurtosis-charon-vc$1-lighthouse-1 /bin/bash -c '\
  for file in /opt/charon/keys/*; do \
    filename=$(basename $file);
    if [[ $filename == *".json"* ]]; then
      keystore=${filename%.*};
      lighthouse account validator exit \
        --beacon-node http://node$0:3600 \
        --keystore /opt/charon/keys/$keystore.json \
        --testnet-dir /opt/lighthouse/network-configs/ \
        --password-file /opt/charon/keys/$keystore.txt \
        --no-confirmation \
        --no-wait;
    fi;
  done;' $1
