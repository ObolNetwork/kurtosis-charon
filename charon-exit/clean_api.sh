#!/usr/bin/env bash

lock_hash=$(echo "$(cat ./.charon/cluster/node0/cluster-lock.json)" | jq -r '.lock_hash')
api="http://localhost:3000/v1"

echo "$(cat ./.charon/cluster/node0/cluster-lock.json)" \
    | jq -r '.distributed_validators.[].distributed_public_key' \
    | while read -r validator; do \
        echo "Deleting partial exits for validator $validator for all nodes"; \
        curl -s -X 'DELETE' "$api/exp/exit/$lock_hash/1/$validator" 1> /dev/null; \
        curl -s -X 'DELETE' "$api/exp/exit/$lock_hash/2/$validator" 1> /dev/null; \
        curl -s -X 'DELETE' "$api/exp/exit/$lock_hash/3/$validator" 1> /dev/null; \
    done;
