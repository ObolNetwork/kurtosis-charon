#!/usr/bin/env bash

requested_CL_TYPE=${CL_TYPE-}
requested_CL_IMAGE=${CL_IMAGE-}
requested_CL_VERSION=${CL_VERSION-}

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

if [ -n "$requested_CL_TYPE" ]; then
  CL_TYPE=$requested_CL_TYPE
  if [ -n "$requested_CL_IMAGE" ]; then
    CL_IMAGE=$requested_CL_IMAGE
  else
    unset CL_IMAGE
  fi

  if [ -n "$requested_CL_VERSION" ]; then
    CL_VERSION=$requested_CL_VERSION
  else
    unset CL_VERSION
  fi
fi

# Concatenate the CL-specific network params and the general network params and write them to network_params.yaml.
cat "./deployments/network_params/network_params_${CL_TYPE}.yaml" ./deployments/network_params/network_params_base.yaml >network_params.yaml

# If CL_IMAGE or CL_VERSION is not set, read ./deployments/env/cl_${CL_TYPE}.env.
if [ -z ${CL_IMAGE+x} ] || [ -z ${CL_VERSION+x} ]; then
  dir="./deployments/env/cl_${CL_TYPE}.env"
  echo "CL_IMAGE or CL_VERSION is unset, reading from ${dir}"
  env_CL_IMAGE=$CL_IMAGE
  env_CL_VERSION=$CL_VERSION
  export $(xargs <$dir)
  CL_IMAGE=${env_CL_IMAGE:-$CL_IMAGE}
  CL_VERSION=${env_CL_VERSION:-$CL_VERSION}
fi

# If EL_IMAGE or EL_VERSION is not set, read ./deployments/env/el_${EL_TYPE}.env.
EL_TYPE=${EL_TYPE:-"geth"}
if [ -z ${EL_IMAGE+x} ] || [ -z ${EL_VERSION+x} ]; then
  dir="./deployments/env/el_${EL_TYPE}.env"
  echo "EL_IMAGE or EL_VERSION is unset, reading from ${dir}"
  env_EL_IMAGE=$EL_IMAGE
  env_EL_VERSION=$EL_VERSION
  export $(xargs <$dir)
  EL_IMAGE=${env_EL_IMAGE:-$EL_IMAGE}
  EL_VERSION=${env_EL_VERSION:-$EL_VERSION}
fi

# Substitute versions in network_params.
envsubst <"network_params.yaml" >"network_params.yaml.tmp"
mv "network_params.yaml.tmp" "network_params.yaml"

upsert_env CL_TYPE "${CL_TYPE}"
upsert_env CL_IMAGE "${CL_IMAGE}"
upsert_env CL_VERSION "${CL_VERSION}"

# Add CHARON_EXTRA_RUN_ARGS for nimbus CL. For more information read the README.md.
if [[ "$CL_TYPE" == "nimbus" ]]; then
  if ! [ -f .env ]; then
    echo "CHARON_EXTRA_RUN_ARGS=--feature-set-enable=json_requests" >>./.env
  fi
fi
