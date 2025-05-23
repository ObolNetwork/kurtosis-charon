#!/usr/bin/env bash

# Load .env if it exists.
if ! [ -f .env ]; then
  echo ".env does not exist, using supplied env variables."
else
  echo "Loading .env file."
  export $(xargs <.env)
fi

# Concatenate the CL-specific network params and the general network params and write them to network_params.yaml.
cat "./deployments/network_params/network_params_${CL_TYPE}.yaml" ./deployments/network_params/network_params_base.yaml >network_params.yaml

# If CL_VERSION is not set, read ./deployments/env/cl_${CL_TYPE}.env.
if [ -z ${CL_VERSION+x} ]; then
  dir="./deployments/env/cl_${CL_TYPE}.env"
  echo "CL_VERSION is unset, reading from ${dir}"
  export $(xargs <$dir)
fi

# If EL_VERSION is not set, read ./deployments/env/el_${EL_TYPE}.env.
EL_TYPE=${EL_TYPE:-"geth"}
if [ -z ${EL_VERSION+x} ]; then
  dir="./deployments/env/el_${EL_TYPE}.env"
  echo "EL_VERSION is unset, reading from ${dir}"
  export $(xargs <$dir)
fi

# Substitute versions in network_params.
envsubst <"network_params.yaml" >"network_params.yaml.tmp"
mv "network_params.yaml.tmp" "network_params.yaml"

# Add CHARON_EXTRA_RUN_ARGS for nimbus CL. For more information read the README.md.
if [[ "$CL_TYPE" == "nimbus" ]]; then
  if ! [ -f .env ]; then
    echo "CHARON_EXTRA_RUN_ARGS=--feature-set-enable=json_requests" >>./.env
  fi
fi
