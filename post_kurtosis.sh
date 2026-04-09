#!/usr/bin/env bash

# Post-Kurtosis overrides and container configuration adjustments.
# Run after `kurtosis run` completes to apply any custom policies or settings.

set -e

echo "Running post-Kurtosis setup..."

# Override mev-relay-api restart policy to auto-restart on failure
echo "Setting mev-relay-api restart policy to 'unless-stopped'..."
docker update --restart=unless-stopped mev-relay-api 2>/dev/null || true

echo "Post-Kurtosis setup complete."
