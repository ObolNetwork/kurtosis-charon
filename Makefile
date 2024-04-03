.PHONY: run clean

run-geth-lighthouse:
	rm -f planprint
	kurtosis run --enclave local-eth-testnet github.com/kurtosis-tech/ethereum-package --args-file ./network_params_geth_lighthouse.yaml > planprint
	sleep 10
	./run_charon.sh

run-geth-nimbus:
	rm -f planprint
	kurtosis run --enclave local-eth-testnet github.com/kurtosis-tech/ethereum-package --args-file ./network_params_geth_nimbus.yaml > planprint
	sleep 10
	./run_charon.sh

run-geth-lodestar:
	rm -f planprint
	kurtosis run --enclave local-eth-testnet github.com/kurtosis-tech/ethereum-package --args-file ./network_params_geth_lodestar.yaml > planprint
	sleep 10
	./run_charon.sh

run-geth-prysm:
	rm -f planprint
	kurtosis run --enclave local-eth-testnet github.com/kurtosis-tech/ethereum-package --args-file ./network_params_geth_prysm.yaml > planprint
	sleep 10
	./run_charon.sh

run-geth-teku:
	rm -f planprint
	kurtosis run --enclave local-eth-testnet github.com/kurtosis-tech/ethereum-package --args-file ./network_params_geth_teku.yaml > planprint
	sleep 10
	./run_charon.sh

clean:
	kurtosis enclave stop local-eth-testnet
	kurtosis enclave rm local-eth-testnet
	rm -rf node*
	rm -f planprint
	rm -rf keystore
	docker compose down
