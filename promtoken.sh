#!/usr/bin/env bash

if ((BASH_VERSINFO[0] < 5))
then
  echo "ERROR: You need at least bash-5.0 to run this script."
  exit 1
fi

# Load the YAML configuration file into a variable
config=$(cat prometheus/prometheus.yml)

updated_config=$(echo "$config")

# Check if PROMETHEUS_REMOTE_WRITE_TOKEN environment variable is set
# if it's set, replace the it in the final configuration
if ! [ -z "$PROMETHEUS_REMOTE_WRITE_TOKEN" ]; then
  # Read the PROMETHEUS_REMOTE_WRITE_TOKEN from the environment variable
  token=$PROMETHEUS_REMOTE_WRITE_TOKEN

  # Replace the credentials value in the config with the token
  updated_config=$(echo "$updated_config" | sed "s|credentials: \"\"|credentials: \"$token\"|")
fi

# Check if PROMETHEUS_WRITE_URL environment variable is set
# if it's set, replace the URL with the final configuration
if ! [ -z "$PROMETHEUS_WRITE_URL" ]; then
  url=$PROMETHEUS_WRITE_URL

  updated_config=$(echo "$updated_config" | sed "s|url: https://vm.monitoring.gcp.obol.tech/write|url: $url|g")
fi

# Write the updated configuration back to the YAML file
echo "$updated_config" > prometheus/prometheustmp.yml

echo "Updated YAML configuration with credentials from .env file."
