#!/usr/bin/env bash

if ((BASH_VERSINFO[0] < 5)); then
  echo "ERROR: You need at least bash-5.0 to run this script."
  exit 1
fi

if ! [ -f .env ]; then
  echo ".env does not exist, using supplied env variables."
else
  echo "Loading .env file."
  export $(xargs <.env)
fi

# Check if EXTERNAL_MONITORING environment variable is set
if [ -z "$EXTERNAL_MONITORING" ]; then
  dir="./deployments/env/charon.env"
  echo "EXTERNAL_MONITORING is unset, reading from ${dir}"
  export $(xargs <$dir)
fi

# Check if PROMETHEUS_REMOTE_WRITE_TOKEN environment variable is set
if "$EXTERNAL_MONITORING" && [ -z "$PROMETHEUS_REMOTE_WRITE_TOKEN" ]; then
  echo "PROMETHEUS_REMOTE_WRITE_TOKEN environment variable is not set for external monitoring."
  exit 1
fi

cp prometheus/prometheus.yml prometheus/prometheustmp.yml

if "$EXTERNAL_MONITORING"; then
  echo "remote_write:
  - url: https://vm.monitoring.gcp.obol.tech/write
    authorization:
      credentials: '${PROMETHEUS_REMOTE_WRITE_TOKEN}'
    write_relabel_configs:
      - source_labels: [job]
        regex: 'charon(.*)|otelcollector'
        action: keep" >>prometheus/prometheustmp.yml
fi

echo "Updated YAML configuration with credentials."
