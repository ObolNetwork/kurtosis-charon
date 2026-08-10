#!/usr/bin/env bash

lock=$(cat ./.charon/cluster/node0/cluster-lock.json)
#TODO: use deployed version of the API, instead of a local one
api="http://localhost:3000/v1"
api_docker="http://api:3000/v1"
beacon=${BN_0:-172.16.0.13:4000}

echo "Registering cluster lock at API";
echo $api

curl -s -X "POST" \
  "${api}/lock" \
  -H 'accept: application/json' \
  -H 'Content-Type: application/json' \
  -d "$lock" 1> dev/null;

echo "Signing partial exits for all validators for node0";

docker compose exec -it node0 /bin/sh -c "charon exit sign \
--beacon-node-endpoints=$beacon \
--all \
--publish-address=$api_docker \
--publish-timeout="5m" \
--exit-epoch=2 \
--testnet-chain-id=3151908 \
--testnet-fork-version="0x10000038" \
--testnet-genesis-timestamp="1726762940" \
--testnet-name=kurtosis-testnet \
--testnet-capella-hard-fork="0x40000038" \
--private-key-file=.charon/cluster/node0/charon-enr-private-key \
--lock-file=.charon/cluster/node0/cluster-lock.json \
--validator-keys-dir=.charon/cluster/node0/validator_keys";

echo "Signing partial exits for all validators for node1";

docker compose exec -it node1 /bin/sh -c "charon exit sign \
--beacon-node-endpoints=$beacon \
--all \
--publish-address=$api_docker \
--publish-timeout="5m" \
--exit-epoch=2 \
--testnet-chain-id=3151908 \
--testnet-fork-version="0x10000038" \
--testnet-genesis-timestamp="1726762940" \
--testnet-name=kurtosis-testnet \
--testnet-capella-hard-fork="0x40000038" \
--private-key-file=.charon/cluster/node1/charon-enr-private-key \
--lock-file=.charon/cluster/node1/cluster-lock.json \
--validator-keys-dir=.charon/cluster/node1/validator_keys";

echo "Signing partial exits for all validators for node2";

docker compose exec -it node2 /bin/sh -c "charon exit sign \
--beacon-node-endpoints=$beacon \
--all \
--publish-address=$api_docker \
--publish-timeout="5m" \
--exit-epoch=2 \
--testnet-chain-id=3151908 \
--testnet-fork-version="0x10000038" \
--testnet-genesis-timestamp="1726762940" \
--testnet-name=kurtosis-testnet \
--testnet-capella-hard-fork="0x40000038" \
--private-key-file=.charon/cluster/node2/charon-enr-private-key \
--lock-file=.charon/cluster/node2/cluster-lock.json \
--validator-keys-dir=.charon/cluster/node2/validator_keys";

mkdir ./.charon/fetched_exits;

echo "Fetching full exits for all validators";

docker compose exec -it node0 /bin/sh -c "charon exit fetch \
--publish-address=$beacon \
--private-key-file=.charon/cluster/node0/charon-enr-private-key \
--lock-file=.charon/cluster/node0/cluster-lock.json \
--all \
--fetched-exit-path="/opt/charon/.charon/fetched_exits" \
--publish-timeout="5m" \
--testnet-chain-id=3151908 \
--testnet-fork-version="0x10000038" \
--testnet-genesis-timestamp="1726762940" \
--testnet-name=kurtosis-testnet \
--testnet-capella-hard-fork="0x40000038"";

echo "Broadcasting full exits to beacon node";

docker compose exec -it node0 /bin/sh -c "charon exit broadcast \
--publish-address=$api_docker \
--private-key-file=.charon/cluster/node0/charon-enr-private-key \
--lock-file=.charon/cluster/node0/cluster-lock.json \
--publish-timeout="5m" \
--exit-epoch=2 \
--beacon-node-endpoints=$beacon \
--exit-from-dir=/opt/charon/.charon/fetched_exits \
--all \
--testnet-chain-id=3151908 \
--testnet-fork-version="0x10000038" \
--testnet-genesis-timestamp="1726762940" \
--testnet-name=kurtosis-testnet \
--testnet-capella-hard-fork="0x40000038"";
