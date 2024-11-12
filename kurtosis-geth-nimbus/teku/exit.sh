#!/usr/bin/env bash

docker exec -it kurtosis-charon-vc$1-teku-1 /opt/teku/bin/teku voluntary-exit \
        --beacon-node-api-endpoint="http://node$1:3600/" \
        --confirmation-enabled=false \
        --validator-keys="/opt/charon/validator_keys:/opt/charon/validator_keys"
