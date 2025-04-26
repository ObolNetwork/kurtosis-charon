#!/usr/bin/env bash

if [ -z ${VC_VERSION+x} ]; then
    dir="./deployments/env/vc_${VC_TYPE}.env"
    echo "VC_VERSION is unset, reading from ${dir}"
    export $(xargs <$dir)
fi

if ! [ -f .env ]; then
    echo ".env does not exist, using supplied env variables."
else
    echo "Loading .env file."
    export $(xargs <.env)
fi

if ! grep -q VC_TYPE ./.env; then
    echo "VC_TYPE=${VC_TYPE}" >>./.env
fi

if ! grep -q VC_VERSION ./.env; then
    echo "VC_VERSION=${VC_VERSION}" >>./.env
fi

if [[ "$VC_TYPE" == "lodestar" ]]; then
    mkdir -p data/lodestar/vc{0,1,2}/{caches,keystores,validator-db}
fi

if [[ "$VC_TYPE" == "nimbus" ]]; then
    mkdir -p data/nimbus/vc{0,1,2}
fi
