.PHONY: run-aws stop-aws clean

# Run a single combo locally.
# Usage: make run-local/lighthouse-lodestar
run-local/%:
	kurtosis run --enclave $* github.com/ObolNetwork/ethereum-package@6.1.0-obol --args-file ./deployments/$*.yaml

stop-local/%:
	kurtosis enclave rm -f $*

run-aws:
	./aws/run.sh

stop-aws:
	./aws/stop.sh

clean:
	-kurtosis clean -a
