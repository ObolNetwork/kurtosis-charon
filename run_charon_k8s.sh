#!/usr/bin/env bash

if ((BASH_VERSINFO[0] < 5))
then
  echo "ERROR: You need at least bash-5.0 to run this script."
  exit 1
fi

if [ "$1" = "" ]
then
  echo "Cluster name is missing!!! Please provide cluster name. Example: ./run_charon_k8s.sh <cluster-name>"
  exit
fi

CLUSTER_NAME=$1
CL_NAME=$(echo $CLUSTER_NAME | cut -d'-' -f3)

# Function to check if a cluster directory exists and delete it
delete_directory_if_exists() {
    if [ -d "$1" ]; then
        rm -r "$1"
        echo "Deleted $1"
    fi
}

# Function to find the first directory in a given path
find_first_directory() {
    for dir in "$1"/*; do
        if [ -d "$dir" ]; then
            echo "$dir"
            return
        fi
    done
}

extract_bn_ip() {
    uuid=$1
    kurtosis_inspect_output=$2
    names=$3
    local -n ret=$4 
    ret=()

    idx=1
    for beaconClient in ${names[@]}; do
    # Extract the --enr-address value from the kurtosis service inspect command
    if [ -n "$uuid" ]; then
        kurtosis_inspect_output=$(kurtosis service inspect "$uuid" "$beaconClient")
        echo $kurtosis_inspect_output
        # if $beaconClient contains "lighthouse" then
        if [[ $beaconClient == *"lighthouse"* ]]; then
            enr_address=$(echo "$kurtosis_inspect_output" | awk -F= '/--enr-address=/ {print $2}')
            enr_address_port=$(( $(echo "$kurtosis_inspect_output" | awk -F= '/--http-port=/ {print $2}') + 1 ))
        elif [[ $beaconClient == *"teku"* ]]; then
            enr_address=$(echo "$kurtosis_inspect_output" | awk -F= '/--p2p-advertised-ip=/ {print $2}')
            enr_address_port=$(echo "$kurtosis_inspect_output" | awk -F= '/--rest-api-port=/ {print $2}')
        elif [[ $beaconClient == *"nimbus"* ]]; then
            enr_address=$(echo "$kurtosis_inspect_output" | awk -F: '/--nat=extip:/ {print $2}')
            enr_address_port=$(( $(echo "$kurtosis_inspect_output" | awk -F= '/--http-port=/ {print $2}') + 3 ))
        elif [[ $beaconClient == *"prysm"* ]]; then
            enr_address=$(echo "$kurtosis_inspect_output" | awk -F= '/--p2p-host-ip=/ {print $2}')
            enr_address_port=$(( $(echo "$kurtosis_inspect_output" | awk -F= '/--http-port=/ {print $2}') + 4 ))
        elif [[ $beaconClient == *"lodestar"* ]]; then
            enr_address=$(echo "$kurtosis_inspect_output" | awk -F= '/--enr.ip=/ {print $2}')
            enr_address_port=$(( $(echo "$kurtosis_inspect_output" | awk -F= '/--rest.port=/ {print $2}') + 2 ))
        fi
        echo "Beacon node address found: $enr_address:$enr_address_port"
        ret+=($(echo "host.docker.internal:${idx}${enr_address_port}"))
    else
        echo "UUID not found."
    fi
    idx=$((idx + 1))
    done
    
}

# Delete the 'keystore-keys' folder if it exists
# delete_directory_if_exists "keystore"
# delete_directory_if_exists "charon_keys"
rm -rf testnet
rm -rf keystore
rm -rf charon-keys
rm -rf node*
rm -rf $CLUSTER_NAME
cp .env.sample .env
mkdir -p ./${CLUSTER_NAME}/testnet
mkdir -p ./${CLUSTER_NAME}/.charon/cluster


# Delete the 'keystore-secrets' folder if it exists
# delete_directory_if_exists "testnet-$CLUSTER_NAME"

# Run the kurtosis enclave ls command and capture its output
enclave_output=$(kurtosis enclave ls | grep -E $CLUSTER_NAME)

# Use grep and awk to extract the UUID
uuid=$(echo "$enclave_output" | grep -oE '^[0-9a-f]+ ' | awk '{print $1}')

# Print the extracted UUID
echo "Extracted UUID: $uuid"

# Set a flag to indicate whether we are capturing JSON
capture_json=false

# Initialize a variable to store the JSON content
json_content=""

# Read the file line by line
while IFS= read -r line; do
  # Check if the line contains "Starlark code successfully run. Output was:"
  if [[ "$line" == *"Starlark code successfully run. Output was:"* ]]; then
    capture_json=true
    # Start capturing the JSON
    continue
  elif [[ $capture_json == true && "$line" == "}" ]]; then
    json_content+="}"
    # Stop capturing when we reach the end of the JSON block
    break
  elif [[ $capture_json == true ]]; then
    # Append the line to the JSON content variable
    json_content+="$line"$'\n'
  fi
done < "planprint-$CLUSTER_NAME"

# Print or use the JSON content variable as needed
# Use jq to extract the value of validator_keystore_files_artifact_uuid
uuidValidator=$(echo "$json_content" | jq -r '.all_participants[0].cl_context.validator_keystore_files_artifact_uuid')
beaconClient=$(echo "$json_content" | jq -r '.all_participants[0].cl_context.beacon_service_name')

beaconClients=$(echo "$json_content" | jq -r '.all_participants[].cl_context.beacon_service_name')

if [ -n "$uuid" ]; then
    # Run the kurtosis port print command with the extracted UUID and save the output to a variable
    cl_port=$(kurtosis port print "$uuid" "$beaconClient" http | grep -Eo 'http://[0-9\.]+:[0-9]+')
    
    # Print the output
    echo "Port Print Output: ${cl_port}"

# Print the extracted UUID
echo "Validator Keystore Files Artifact UUID: $uuidValidator"


    # Check if port is not empty
    if [ -n "${cl_port}" ]; then
        # Run the kurtosis files download commands to download the keys
        # kurtosis files download "$uuid" keystore-keys
        kurtosis files download "$uuid" $uuidValidator ./keystore
        kurtosis files download "$uuid" el_cl_genesis_data ./${CLUSTER_NAME}/testnet

        # Use curl to get the JSON response and extract the genesis_time
        url="${cl_port}/eth/v1/beacon/genesis"
        json_response=$(curl -s "$url")
        
        # Extract the genesis_time from the JSON response
        genesis_time=$(echo "$json_response" | jq -r '.data.genesis_time')
        
        # Print the genesis_time
        echo "Genesis Time: $genesis_time"
    else
        echo "Port not found."
    fi
else
    echo "UUID not found."
fi

# Check if the 'charon-keys' directory exists, create it if not
charon_dir="charon-keys"
if [ ! -d "$charon_dir" ]; then
    mkdir "$charon_dir"
else
    rm -r "$charon_dir/*"
fi

# Find the first directory in 'keystore-keys' parent directory
first_keystore_dir=$(find_first_directory "keystore/keys")
echo "First keystore directory: $first_keystore_dir"

# Find all directories in 'keystore-keys/keys'
keystore_directories="keystore/keys/*"

# Counter to track the index
index=1

# Iterate over each directory
for keystore_dir in $keystore_directories; do
    # Check if it's a directory
    if [ -d "$keystore_dir" ]; then
        echo "Processing keystore directory: $keystore_dir"

        # Copy 'voting-keystore.json' to 'charon-keys' with an indexed name
        cp "$keystore_dir/voting-keystore.json" "$charon_dir/keystore-${index}.json"
        echo "Copied 'voting-keystore.json' to 'charon-keys' as 'keystore-${index}.json'"

        # Extract the directory name (pubkey) from the current keystore directory
        dir_name=$(basename "$keystore_dir")

        # Check if a file with the same name exists in 'keystore-secrets' and copy it to 'charon-keys' with an indexed name
        if [ -f "keystore/secrets/$dir_name" ]; then
            cp "keystore/secrets/$dir_name" "$charon_dir/keystore-${index}.txt"
            echo "Copied '$dir_name' from 'keystore-secrets' to 'charon-keys' as 'keystore-${index}.txt'"
        else
            echo "No matching file found in 'keystore-secrets' for '$dir_name'."
        fi

        # Increment the index for the next iteration
        ((index++))
    fi
done

local bnips
extract_bn_ip "$uuid" "$kurtosis_inspect_output" "$beaconClients" bnips

# Remove any existing node* folders
rm -rf node*

# Check if genesis_time is not empty
if [ -n "$genesis_time" ] && [ -n "$enr_address" ]; then
    grep -q "CHARON_BEACON_NODE_ENDPOINTS=" ./.env
    if [ $? -eq 0 ]; then
        cp ./.env ./.env.tmp
        sed '/^CHARON_BEACON_NODE_ENDPOINTS=/d' ./.env.tmp > ./.env
        rm ./.env.tmp
        echo "Removed existing CHARON_BEACON_NODE_ENDPOINTS entry from .env file"
    fi
    # Add the entry to the .env file
    echo "TESTNET_GENESIS_TIME_STAMP=$genesis_time" >> ./.env
    echo "BUILDER_API_ENABLED=true" >> ./.env

    for i in "${!bnips[@]}"; do
        echo "BN_$i=${bnips[$i]}" >> ./.env
    done
    # Run the docker command with the extracted genesis_time
    docker run -u $(id -u):$(id -g) --rm -v "$(pwd)/:/opt/charon" obolnetwork/charon:latest create cluster --fee-recipient-addresses="0x8943545177806ED17B9F23F0a21ee5948eCaa776" --nodes=3 --withdrawal-addresses="0xBc7c960C1097ef1Af0FD32407701465f3c03e407" --name=${CLUSTER_NAME} --split-existing-keys --split-keys-dir=charon-keys --testnet-chain-id=3151908 --testnet-fork-version="0x10000038" --testnet-genesis-timestamp="$genesis_time" --testnet-name=kurtosis-testnet
else
    echo "Genesis Time not found."
fi

echo "NETWORK_NAME=${CLUSTER_NAME}" >> ./.env
## Append the CL_NAME to the .env file
echo "CL_NAME=${CL_NAME}" >> ./.env
# Determine the prometheus port based on the CL_NAME
if [ "$CL_NAME" == "lighthouse" ]; then
  MONITORING_PORT_PROMETHEUS="9090:9090"
elif [ "$CL_NAME" == "lodestar" ]; then
  MONITORING_PORT_PROMETHEUS="9091:9091"
elif [ "$CL_NAME" == "nimbus" ]; then
  MONITORING_PORT_PROMETHEUS="9092:9092"
elif [ "$CL_NAME" == "teku" ]; then
  MONITORING_PORT_PROMETHEUS="9093:9093"
else
  MONITORING_PORT_PROMETHEUS="9094:9094"
fi

# Append the prometheus port to the .env file
echo "MONITORING_PORT_PROMETHEUS=${MONITORING_PORT_PROMETHEUS}" >> ./.env



cp Makefile_k8s ./${CLUSTER_NAME}/Makefile
cp -r node* ./${CLUSTER_NAME}/.charon/cluster
cp .env ./${CLUSTER_NAME}/.env
#if [ "$CL_NAME" == "lighthouse" ]; then
    cp -r lighthouse ./${CLUSTER_NAME}/lighthouse
#fi
#if [ "$CL_NAME" == "teku" ]; then
    cp -r teku ./${CLUSTER_NAME}/teku
#fi
#if [ "$CL_NAME" == "prysm" ]; then
    cp -r prysm ./${CLUSTER_NAME}/prysm
#fi
#if [ "$CL_NAME" == "nimbus" ]; then
    cp -r nimbus ./${CLUSTER_NAME}/nimbus
#fi
    cp -r lodestar ./${CLUSTER_NAME}/lodestar
cp -r prometheus ./${CLUSTER_NAME}/prometheus
cp docker-compose-k8.yml ./${CLUSTER_NAME}/docker-compose.yml
rm -rf node*
rm -rf keystore*
rm -rf charon-keys
rm .env

# Save the k8s VC pods configuration to a YAML file
kubectl get pod vc-1-geth-${CL_NAME} -n kt-${CLUSTER_NAME} -o yaml > ./${CLUSTER_NAME}/vc-1-geth-${CL_NAME}.yaml && echo "Saving k8s VC pod config file: vc-1-geth-${CL_NAME}.yaml"
kubectl get pod vc-2-geth-${CL_NAME} -n kt-${CLUSTER_NAME} -o yaml > ./${CLUSTER_NAME}/vc-2-geth-${CL_NAME}.yaml && echo "Saving k8s VC pod config file: vc-2-geth-${CL_NAME}.yaml"
kubectl get pod vc-3-geth-${CL_NAME} -n kt-${CLUSTER_NAME} -o yaml > ./${CLUSTER_NAME}/vc-3-geth-${CL_NAME}.yaml && echo "Saving k8s VC pod config file: vc-3-geth-${CL_NAME}.yaml"
# Save the k8s VC service configuration to a YAML file
kubectl get service vc-1-geth-${CL_NAME} -n kt-${CLUSTER_NAME} -o yaml > ./${CLUSTER_NAME}/vc-1-geth-${CL_NAME}-service.yaml && echo "Saving k8s VC service config file: vc-1-geth-${CL_NAME}-service.yaml"
kubectl get service vc-2-geth-${CL_NAME} -n kt-${CLUSTER_NAME} -o yaml > ./${CLUSTER_NAME}/vc-2-geth-${CL_NAME}-service.yaml && echo "Saving k8s VC service config file: vc-2-geth-${CL_NAME}-service.yaml"
kubectl get service vc-3-geth-${CL_NAME} -n kt-${CLUSTER_NAME} -o yaml > ./${CLUSTER_NAME}/vc-3-geth-${CL_NAME}-service.yaml && echo "Saving k8s VC service config file: vc-3-geth-${CL_NAME}-service.yaml"
# Delete all k8s VC pods.
kubectl get pods -n kt-${CLUSTER_NAME} --no-headers | awk '/^vc-/{print $1}' | xargs -I {} kubectl delete pod {} -n kt-${CLUSTER_NAME}
# Delete all k8s VC services.
kubectl get services -n kt-${CLUSTER_NAME} --no-headers | awk '/^vc-/{print $1}' | xargs -I {} kubectl delete service {} -n kt-${CLUSTER_NAME}
