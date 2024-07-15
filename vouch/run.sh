#!/usr/bin/env bash

apt-get update && apt-get install -y jq

# Move template files to working dir
rm -rf /opt/charon/vouch_base_dir
mkdir -p /opt/charon/vouch_base_dir
cp -rf /opt/charon/vouch/vouch.yml /opt/charon/vouch_base_dir/vouch.yml
cp -rf /opt/charon/vouch/8c9b98b8-0496-48a3-96f1-4fe08ddfaae1 /opt/charon/vouch_base_dir/8c9b98b8-0496-48a3-96f1-4fe08ddfaae1

# Add keys to vouch dir
index_path="/opt/charon/vouch_base_dir/8c9b98b8-0496-48a3-96f1-4fe08ddfaae1/index"
for f in /opt/charon/validator_keys/keystore-*.json; do
    echo "Importing key ${f}"

    # Extract the uuid from the key
    uuid=$(jq -r '.uuid' $f | tr '[:upper:]' '[:lower:]')
    basen=$(basename $f)
    filename=${basen%.*}

    # Update indexer with the key
    jq --arg uuid "$uuid" --arg name "$filename" '. += [{
        "uuid": $uuid,
        "name": $name
    }]' /opt/charon/vouch_base_dir/8c9b98b8-0496-48a3-96f1-4fe08ddfaae1/index > $index_path.tmp
    mv $index_path.tmp $index_path

    # Copy the keystore file and update missing fields
    new_file_path="/opt/charon/vouch_base_dir/8c9b98b8-0496-48a3-96f1-4fe08ddfaae1/$uuid"
    cp $f $new_file_path

    jq --arg filename "$filename" '.name += $filename' $new_file_path > $new_file_path.tmp
    mv $new_file_path.tmp $new_file_path

    jq --arg uuid "$uuid" '.uuid = $uuid' $new_file_path > $new_file_path.tmp
    mv $new_file_path.tmp $new_file_path
done

# Add passphrases to vouch.yml
for f in /opt/charon/validator_keys/keystore-*.txt; do
    line="\ \ \ \ \ \ - file://$f"
    sed -i "\|   passphrases:|a $line" /opt/charon/vouch_base_dir/vouch.yml
done

# Add BN address
echo $BEACON_NODE_ADDRESS
sed -i "s|beacon-node-address:|beacon-node-address: $BEACON_NODE_ADDRESS|" /opt/charon/vouch_base_dir/vouch.yml

# Run vouch
/app/vouch --base-dir="/opt/charon/vouch_base_dir" --log-level=debug
