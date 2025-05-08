#!/usr/bin/env bash

apt-get update
apt-get install -y wget curl

# Install yq
wget https://github.com/mikefarah/yq/releases/latest/download/yq_linux_amd64 -O /usr/bin/yq
chmod +x /usr/bin/yq

# Install ethdo
wget https://github.com/wealdtech/ethdo/releases/download/v1.37.3/ethdo-1.37.3-linux-amd64.tar.gz
tar -xvf ethdo-1.37.3-linux-amd64.tar.gz
rm ethdo-1.37.3-linux-amd64.tar.gz

# Passphrase for every account in this wallet
account_passphrase="1234"

./ethdo wallet create --wallet=vals --passphrase=""

accounts_list=()
for keystore_file in /home/charon/validator_keys/keystore-*.json; do
    basename="$(basename "${keystore_file%.json}")"
    password_file="/home/charon/validator_keys/${basename}.txt"
    index="${basename##*-}"
    account_name="vals/val${index}"
    passphrase_content=$(cat "$password_file")

    echo "Import account with name $account_name with keystore $keystore_file and passphrase $passphrase_content"
    ./ethdo account import \
        --account="$account_name" \
        --keystore="$keystore_file" \
        --keystore-passphrase="$passphrase_content" \
        --passphrase="$account_passphrase" --allow-weak-passphrases

    accounts_list+=("$account_name")
done

yq_accounts=$(printf "      - %s\n" "${accounts_list[@]}")
echo -n "$account_passphrase" >> /opt/vouch/account_passphrase.txt

cat > ~/.vouch.yml <<EOF
beacon-node-address: $BEACON_NODE_ADDRESS
log-level: "debug"
accountmanager:
  wallet:
    accounts:
$yq_accounts
    passphrases:
      - file:///opt/vouch/account_passphrase.txt
blockrelay:
  fallback-fee-recipient: "0x0000000000000000000000000000000000000001"
metrics:
  prometheus:
    listen-address: "127.0.0.1:12345"
EOF

./vouch
