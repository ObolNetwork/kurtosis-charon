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

# Write the VC_IMAGE that was previously loaded to the .env, if it's not written.
if ! grep -q VC_IMAGE ./.env; then
    echo "VC_IMAGE=${VC_IMAGE}" >>./.env
fi

# Write the VC_VERSION that was previously loaded to the .env, if it's not written.
if ! grep -q VC_VERSION ./.env; then
    echo "VC_VERSION=${VC_VERSION}" >>./.env
fi

# Create data folders for lodestar VC.
if [[ "$VC_TYPE" == "lodestar" ]]; then
    mkdir -p data/lodestar/vc{0,1,2}/{caches,keystores,validator-db}
fi

# Nimbus VC Dockerfile copies nimbus_beacon_node from the nimbus CL image.
# Write CL_IMAGE and CL_VERSION from the nimbus CL env so docker compose can pass them as build args.
if [[ "$VC_TYPE" == "nimbus" ]]; then
    mkdir -p data/nimbus/vc{0,1,2}
    export $(xargs <./deployments/env/cl_nimbus.env)
    if ! grep -q CL_IMAGE ./.env; then
        echo "CL_IMAGE=${CL_IMAGE}" >>./.env
    fi
    if ! grep -q CL_VERSION ./.env; then
        echo "CL_VERSION=${CL_VERSION}" >>./.env
    fi
fi
