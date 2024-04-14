#!/bin/bash

# Check if PROMETHEUS_REMOTE_WRITE_TOKEN environment variable is set
if [ -z "$PROMETHEUS_REMOTE_WRITE_TOKEN" ]; then
  echo "Error: PROMETHEUS_REMOTE_WRITE_TOKEN environment variable is not set."
  exit 1
fi

# Read the PROMETHEUS_REMOTE_WRITE_TOKEN from the environment variable
token=$PROMETHEUS_REMOTE_WRITE_TOKEN

# Load the YAML configuration file into a variable
config=$(cat prometheus/prometheus.yml)

# Replace the credentials value in the config with the token
updated_config=$(echo "$config" | sed "s/credentials: \"\"/credentials: \"$token\"/")

# Write the updated configuration back to the YAML file
echo "$updated_config" > prometheus/prometheustmp.yml

echo "Updated YAML configuration with credentials from .env file."
