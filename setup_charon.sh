#!/usr/bin/env bash

# Find the first directory in a given path.
find_first_directory() {
    for dir in "$1"/*; do
        if [ -d "$dir" ]; then
            echo "$dir"
            return
        fi
    done
}

# Extract the IPs and ports of the supplied beacon nodes, from the inside of the Docker network.
extract_bn_ip() {
    uuid=$1
    kurtosis_inspect_output=$2
    names=$3
    local -n ret=$4
    ret=()

    # Iterate over the supplied beacon nodes.
    for beaconClient in ${names[@]}; do
        # Fetch the container's IP address.
        container_id=$(docker ps -aqf "name=$beaconClient")
        bn_ip=$(docker inspect -f '{{range.NetworkSettings.Networks}}{{.IPAddress}}{{end}}' $container_id)

        # Fetch the beacon node port, based on the BN's start command flag.
        if [ -n "$uuid" ]; then
            kurtosis_inspect_output=$(kurtosis service inspect "$uuid" "$beaconClient")
            # if $beaconClient contains "X" then fetch its HTTP port from start command
            if [[ $beaconClient == *"lighthouse"* ]]; then
                bn_port=$(echo "$kurtosis_inspect_output" | sed -n -e 's/^.*http-port=//p' | sed 's/ .*//')
            elif [[ $beaconClient == *"teku"* ]]; then
                bn_port=$(echo "$kurtosis_inspect_output" | sed -n -e 's/^.*rest-api-port=//p' | sed 's/ .*//')
            elif [[ $beaconClient == *"nimbus"* ]]; then
                bn_port=$(echo "$kurtosis_inspect_output" | sed -n -e 's/^.*rest-port=//p' | sed 's/ .*//')
            elif [[ $beaconClient == *"prysm"* ]]; then
                bn_port=$(echo "$kurtosis_inspect_output" | sed -n -e 's/^.*http-port=//p' | sed 's/ .*//')
            elif [[ $beaconClient == *"lodestar"* ]]; then
                bn_port=$(echo "$kurtosis_inspect_output" | sed -n -e 's/^.*rest.port=//p' | sed 's/ .*//')
            elif [[ $beaconClient == *"grandine"* ]]; then
                bn_port=$(echo "$kurtosis_inspect_output" | sed -n -e 's/^.*http-port=//p' | sed 's/ .*//')
            fi
            echo "Beacon node address found: $bn_ip:$bn_port"
            ret+=($(echo "$bn_ip:$bn_port"))
        else
            echo "UUID not found."
        fi

    done

}

if ((BASH_VERSINFO[0] < 5)); then
    echo "ERROR: You need at least bash-5.0 to run this script."
    exit 1
fi

# Load .env if it exists.
if ! [ -f .env ]; then
    echo ".env does not exist, using supplied env variables."
else
    echo "Loading .env file."
    export $(xargs <.env)
fi

# If CHARON_VERSION is not set, read ./deployments/env/charon.env.
if [ -z ${CHARON_VERSION+x} ]; then
    dir="./deployments/env/charon.env"
    echo "CHARON_VERSION is unset, reading from ${dir}"
    export $(xargs <$dir)
fi

# data folder will be used for multiple purpose further down the script.
mkdir -p data

# Run the kurtosis enclave ls command and capture its output.
enclave_output=$(kurtosis enclave ls)

# Use grep and awk to extract the UUID.
uuid=$(echo "$enclave_output" | grep -oE '^[0-9a-f]+ ' | awk '{print $1}')

# Print the extracted UUID.
echo "Extracted enclave UUID: $uuid"

# Set a flag to indicate whether we are capturing JSON.
capture_json=false

# Initialize a variable to store the JSON content.
json_content=""

# Read the file line by line.
while IFS= read -r line; do
    # Check if the line contains "Starlark code successfully run. Output was:".
    if [[ "$line" == *"Starlark code successfully run. Output was:"* ]]; then
        capture_json=true
        # Start capturing the JSON.
        continue
    elif [[ $capture_json == true && "$line" == "}" ]]; then
        json_content+="}"
        # Stop capturing when we reach the end of the JSON block.
        break
    elif [[ $capture_json == true ]]; then
        # Append the line to the JSON content variable.
        json_content+="$line"$'\n'
    fi
done <"planprint"

# Print or use the JSON content variable as needed.
# Use jq to extract the value of validator_keystore_files_artifact_uuid.
uuidValidator=$(echo "$json_content" | jq -r '.all_participants[0].cl_context.validator_keystore_files_artifact_uuid')
beaconClient=$(echo "$json_content" | jq -r '.all_participants[0].cl_context.beacon_service_name')

beaconClients=$(echo "$json_content" | jq -r '.all_participants[].cl_context.beacon_service_name')

if [ -n "$uuid" ]; then
    # Run the kurtosis port print command with the extracted UUID and save the output to a variable.
    cl_port=$(kurtosis port print "$uuid" "$beaconClient" http)

    # Print the output.
    echo "Port Print Output: ${cl_port}"

    # Print the extracted UUID.
    echo "Validator Keystore Files Artifact UUID: $uuidValidator"

    # Check if port is not empty.
    if [ -n "${cl_port}" ]; then
        # Run the kurtosis files download commands to download the keys.
        # kurtosis files download "$uuid" keystore-keys.
        kurtosis files download "$uuid" $uuidValidator ./keystore
        kurtosis files download "$uuid" el_cl_genesis_data ./testnet

        # Use curl to get the JSON response and extract the genesis_time.
        url="${cl_port}/eth/v1/beacon/genesis"
        json_response=$(curl -s "$url")

        # Extract the genesis_time from the JSON response.
        genesis_time=$(echo "$json_response" | jq -r '.data.genesis_time')

        # Print the genesis_time.
        echo "Genesis Time: $genesis_time"
    else
        echo "Port not found."
    fi
else
    echo "UUID not found."
fi

# Check if the 'charon-keys' directory exists, create it if not.
charon_dir="charon-keys"
mkdir "$charon_dir"

# Find all directories in 'keystore-keys/keys'.
keystore_directories="keystore/keys/*"

index=0
echo "Processing keystores from ${keystore_directories}"

# Iterate over each directory.
for keystore_dir in $keystore_directories; do
    # Check if it's a directory.
    if [ -d "$keystore_dir" ]; then

        # Copy 'voting-keystore.json' to 'charon-keys' with an indexed name.
        cp "$keystore_dir/voting-keystore.json" "$charon_dir/keystore-${index}.json"

        # Extract the directory name (pubkey) from the current keystore directory.
        dir_name=$(basename "$keystore_dir")

        # Check if a file with the same name exists in 'keystore-secrets' and copy it to 'charon-keys' with an indexed name.
        if [ -f "keystore/secrets/$dir_name" ]; then
            cp "keystore/secrets/$dir_name" "$charon_dir/keystore-${index}.txt"
        else
            echo "No matching file found in 'keystore-secrets' for '$dir_name'."
        fi

        # Increment the index for the next iteration.
        ((index++))
    fi
done

# Extract BN IP:port and write to `bnips` variable.
extract_bn_ip "$uuid" "$kurtosis_inspect_output" "$beaconClients" bnips

# Create .env file if it doesn't exist
if ! test -f ./.env; then
    touch ./.env
fi

# Write the CHARON_VERSION that was previously loaded to the .env, if it's not written.
if ! grep -q CHARON_VERSION ./.env; then
    echo "CHARON_VERSION=${CHARON_VERSION}" >>./.env
fi

# Check if genesis_time is not empty.
if [ -n "$genesis_time" ]; then
    # Write the entry to the .env file.
    echo "TESTNET_GENESIS_TIME_STAMP=$genesis_time" >>./.env

    # Write BNs IP:port to the .env file.
    for i in "${!bnips[@]}"; do
        echo "BN_$i=${bnips[$i]}" >>./.env
    done

    # Create charon cluster.
    docker run -u $(id -u):$(id -g) --rm -v "$(pwd)/:/opt/charon" obolnetwork/charon:"${CHARON_VERSION}" create cluster \
        --name=test \
        --nodes=3 \
        --fee-recipient-addresses="0x8943545177806ED17B9F23F0a21ee5948eCaa776" \
        --withdrawal-addresses="0xBc7c960C1097ef1Af0FD32407701465f3c03e407" \
        --split-existing-keys \
        --split-keys-dir=charon-keys \
        --testnet-chain-id=3151908 \
        --testnet-fork-version="0x10000038" \
        --testnet-genesis-timestamp="$genesis_time" \
        --testnet-name=kurtosis-testnet
else
    echo "Genesis Time not found."
fi

# Find the Kurtosis network starting with "kt-" from `docker network ls`.
network_name=$(docker network ls --format '{{.Name}}' | grep -oE 'kt-[a-zA-Z0-9_-]+')

# Check if a network name was found.
if [ -n "$network_name" ]; then
    echo "Found network: $network_name"
    # Write the network name to the .env file.
    echo "NETWORK_NAME=$network_name" >>./.env
else
    echo "Network starting with 'kt-' not found."
fi

# Find first Kurtosis VC and kill it.
echo "Killing first VC that was started by Kurtosis"
docker kill $(docker container ls -f "NAME=vc-1-*" --format '{{.Names}}')

# Move the Charon keys to .charon.
mkdir -p ./.charon/cluster
cp -r node* ./.charon/cluster
rm -rf node*

# Print the cluster lock hash.
lock_hash=$(cat ".charon/cluster/node0/cluster-lock.json" | jq -r '.lock_hash')
echo "Cluster lock hash ${lock_hash}"
