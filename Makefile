.PHONY: run-native stop-native run-aws stop-aws clean

# Run a single native Charon combo locally.
# Usage: make run-native COMBO=lighthouse-lighthouse
COMBO ?= lighthouse-lighthouse

run-native:
	kurtosis run --enclave $(COMBO) github.com/ObolNetwork/ethereum-package@charon --args-file ./deployments/$(COMBO).yaml

stop-native:
	kurtosis enclave rm -f $(COMBO)

run-aws:
	./run_aws.sh

stop-aws:
	./stop_aws.sh

clean:
	-kurtosis clean -a
