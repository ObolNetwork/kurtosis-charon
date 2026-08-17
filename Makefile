.PHONY: start-aws stop-aws status-aws start-runner stop-runner status-runner status-local clean

# --- Local (single combo) ---
# Usage: make start-local/lighthouse-lodestar
# Override the Charon image tag with CHARON_VERSION=<tag>.
CHARON_VERSION ?= v1.10.3

start-local/%:
	CHARON_VERSION="$(CHARON_VERSION)" \
		envsubst '$$CHARON_VERSION $$PROMETHEUS_REMOTE_WRITE_TOKEN' \
		< ./deployments/$*.yaml > /tmp/$*.yaml
	kurtosis run --enclave $* github.com/ObolNetwork/ethereum-package@6.1.0-obol.2 --args-file /tmp/$*.yaml

stop-local/%:
	kurtosis enclave rm -f $*

status-local:
	kurtosis enclave ls

# --- AWS fleet ---
start-aws:
	./aws/run.sh

stop-aws:
	./aws/stop.sh

status-aws:
	./aws/status.sh

# --- Runner (continuous local loop) ---
start-runner:
	cd local/runner && ./start.sh

stop-runner:
	cd local/runner && ./stop.sh

status-runner:
	cd local/runner && ./status.sh

# --- Cleanup ---
clean:
	-kurtosis clean -a
