#!/usr/bin/env bash

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

# If EXTERNAL_MONITORING is not set, read ./deployments/env/charon.env.
if [ -z "$EXTERNAL_MONITORING" ]; then
  dir="./deployments/env/charon.env"
  echo "EXTERNAL_MONITORING is unset, reading from ${dir}"
  export $(xargs <$dir)
fi

# Error if EXTERNAL_MONITORING is true and PROMETHEUS_REMOTE_WRITE_TOKEN is not set.
if "$EXTERNAL_MONITORING" && [ -z "$PROMETHEUS_REMOTE_WRITE_TOKEN" ]; then
  echo "PROMETHEUS_REMOTE_WRITE_TOKEN environment variable is not set for external monitoring."
  exit 1
fi

# Copy the generic prometheus.yml to prometheustmp.yml.
cp prometheus/prometheus.yml prometheus/prometheustmp.yml

# If EXTERNAL_MONITORING, add the Obol remote_write config.
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
