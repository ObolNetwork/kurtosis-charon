#!/usr/bin/env bash

docker exec -it kurtosis-charon-vc$1-lodestar-1 node /usr/app/packages/cli/bin/lodestar validator voluntary-exit \
        --paramsFile=/opt/lodestar/config.yaml \
        --beaconNodes="http://node$1:3600" \
        --dataDir=/opt/data \
        --yes
