#!/usr/bin/env bash
set -euo pipefail

PLANPRINT_FILE="${PLANPRINT_FILE:-planprint}"
ENCLAVE_NAME="${ENCLAVE_NAME:-local-eth-testnet}"
WAIT_BEACON_TIMEOUT_SECONDS="${WAIT_BEACON_TIMEOUT_SECONDS:-300}"
WAIT_BEACON_INTERVAL_SECONDS="${WAIT_BEACON_INTERVAL_SECONDS:-5}"
WAIT_BEACON_CURL_TIMEOUT_SECONDS="${WAIT_BEACON_CURL_TIMEOUT_SECONDS:-5}"
WAIT_BEACON_HEALTH_CODES="${WAIT_BEACON_HEALTH_CODES:-200 206}"

capture_planprint_json() {
    awk '
        /Starlark code successfully run\. Output was:/ {
            capture = 1
            next
        }
        capture {
            print
            opens = $0
            closes = $0
            depth += gsub(/\{/, "{", opens)
            depth -= gsub(/\}/, "}", closes)
            if (depth == 0 && $0 ~ /\}/) {
                exit
            }
        }
    ' "$PLANPRINT_FILE"
}

health_code_is_accepted() {
    local health_code=$1

    for accepted_code in $WAIT_BEACON_HEALTH_CODES; do
        if [[ "$health_code" == "$accepted_code" ]]; then
            return 0
        fi
    done

    return 1
}

if [[ ! -f "$PLANPRINT_FILE" ]]; then
    echo "ERROR: ${PLANPRINT_FILE} not found; cannot discover beacon node services."
    exit 1
fi

uuid=$(kurtosis enclave ls | awk -v enclave="$ENCLAVE_NAME" '$0 ~ enclave {print $1; exit}')
if [[ -z "$uuid" ]]; then
    echo "ERROR: Enclave '${ENCLAVE_NAME}' not found."
    exit 1
fi

json_content=$(capture_planprint_json)
if ! echo "$json_content" | jq -e . >/dev/null; then
    echo "ERROR: Could not parse Kurtosis output JSON from ${PLANPRINT_FILE}."
    exit 1
fi

mapfile -t beacon_clients < <(echo "$json_content" | jq -r '.all_participants[].cl_context.beacon_service_name')
if [[ "${#beacon_clients[@]}" -eq 0 ]]; then
    echo "ERROR: No beacon services found in ${PLANPRINT_FILE}."
    exit 1
fi

deadline=$((SECONDS + WAIT_BEACON_TIMEOUT_SECONDS))

for beacon_client in "${beacon_clients[@]}"; do
    beacon_url=$(kurtosis port print "$uuid" "$beacon_client" http)
    health_url="${beacon_url}/eth/v1/node/health"

    echo "Waiting for ${beacon_client} health endpoint: ${health_url}"

    while true; do
        health_code=$(curl -sS --max-time "$WAIT_BEACON_CURL_TIMEOUT_SECONDS" -o /dev/null -w "%{http_code}" "$health_url" || true)
        if health_code_is_accepted "$health_code"; then
            echo "${beacon_client} is healthy enough for Charon startup (HTTP ${health_code})."
            break
        fi

        if ((SECONDS >= deadline)); then
            echo "ERROR: Timed out waiting for ${beacon_client}; last health code was HTTP ${health_code}."
            exit 1
        fi

        sleep "$WAIT_BEACON_INTERVAL_SECONDS"
    done
done
