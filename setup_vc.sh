#!/usr/bin/env bash

# Load .env if it exists.
if ! [ -f .env ]; then
    echo ".env does not exist, using supplied env variables."
else
    echo "Loading .env file."
    export $(xargs <.env)
fi

# If VC_VERSION is not set, read ./deployments/env/cl_${VC_TYPE}.env.
if [ -z ${VC_VERSION+x} ]; then
    dir="./deployments/env/vc_${VC_TYPE}.env"
    echo "VC_VERSION is unset, reading from ${dir}"
    export $(xargs <$dir)
fi

# Write the VC_TYPE that was previously loaded to the .env, if it's not written.
if ! grep -q VC_TYPE ./.env; then
    echo "VC_TYPE=${VC_TYPE}" >>./.env
fi

# Write the VC_VERSION that was previously loaded to the .env, if it's not written.
if ! grep -q VC_VERSION ./.env; then
    echo "VC_VERSION=${VC_VERSION}" >>./.env
fi

# Create data folders for lodestar VC.
if [[ "$VC_TYPE" == "lodestar" ]]; then
    mkdir -p data/lodestar/vc{0,1,2}/{caches,keystores,validator-db}
fi

# Create data folders for nimbus VC.
if [[ "$VC_TYPE" == "nimbus" ]]; then
    mkdir -p data/nimbus/vc{0,1,2}
fi
