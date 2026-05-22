#!/usr/bin/env bash

requested_VC_TYPE=${VC_TYPE-}
requested_VC_IMAGE=${VC_IMAGE-}
requested_VC_VERSION=${VC_VERSION-}

upsert_env() {
    local key=$1
    local value=$2

    touch ./.env
    if grep -q "^${key}=" ./.env; then
        sed -i.bak "s#^${key}=.*#${key}=${value}#" ./.env
        rm -f ./.env.bak
    else
        echo "${key}=${value}" >>./.env
    fi
}

# Load .env if it exists.
if ! [ -f .env ]; then
    echo ".env does not exist, using supplied env variables."
else
    echo "Loading .env file."
    export $(xargs <.env)
fi

if [ -n "$requested_VC_TYPE" ]; then
    VC_TYPE=$requested_VC_TYPE
    if [ -n "$requested_VC_IMAGE" ]; then
        VC_IMAGE=$requested_VC_IMAGE
    else
        unset VC_IMAGE
    fi

    if [ -n "$requested_VC_VERSION" ]; then
        VC_VERSION=$requested_VC_VERSION
    else
        unset VC_VERSION
    fi
fi

# If VC_IMAGE or VC_VERSION is not set, read ./deployments/env/vc_${VC_TYPE}.env.
if [ -z ${VC_IMAGE+x} ] || [ -z ${VC_VERSION+x} ]; then
    dir="./deployments/env/vc_${VC_TYPE}.env"
    echo "VC_IMAGE or VC_VERSION is unset, reading from ${dir}"
    env_VC_IMAGE=$VC_IMAGE
    env_VC_VERSION=$VC_VERSION
    export $(xargs <$dir)
    VC_IMAGE=${env_VC_IMAGE:-$VC_IMAGE}
    VC_VERSION=${env_VC_VERSION:-$VC_VERSION}
fi

upsert_env VC_TYPE "${VC_TYPE}"
upsert_env VC_IMAGE "${VC_IMAGE}"
upsert_env VC_VERSION "${VC_VERSION}"

# Create data folders for lodestar VC.
if [[ "$VC_TYPE" == "lodestar" ]]; then
    mkdir -p data/lodestar/vc{0,1,2}/{caches,keystores,validator-db}
fi

# Create data folders for nimbus VC and persist BN image for Dockerfile build.
if [[ "$VC_TYPE" == "nimbus" ]]; then
    mkdir -p data/nimbus/vc{0,1,2}
    if ! grep -q NIMBUS_BN_IMAGE ./.env; then
        echo "NIMBUS_BN_IMAGE=${NIMBUS_BN_IMAGE}" >>./.env
    fi
fi
