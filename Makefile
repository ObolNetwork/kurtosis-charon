.PHONY: run clean

geth-lighthouse:
	rm -f planprint
	kurtosis run --enclave local-eth-testnet github.com/kurtosis-tech/ethereum-package --args-file ./network_params_geth_lighthouse.yaml > planprint

geth-nimbus:
	rm -f planprint
	kurtosis run --enclave local-eth-testnet github.com/kurtosis-tech/ethereum-package --args-file ./network_params_geth_nimbus.yaml > planprint

geth-lodestar:
	rm -f planprint
	kurtosis run --enclave local-eth-testnet github.com/kurtosis-tech/ethereum-package --args-file ./network_params_geth_lodestar.yaml > planprint

geth-prysm:
	rm -f planprint
	kurtosis run --enclave local-eth-testnet github.com/kurtosis-tech/ethereum-package --args-file ./network_params_geth_prysm.yaml > planprint

geth-teku:
	rm -f planprint
	kurtosis run --enclave local-eth-testnet github.com/kurtosis-tech/ethereum-package --args-file ./network_params_geth_teku.yaml > planprint

charon:
	./run_charon.sh

run-charon-lighthouse:
	docker compose up node0 node1 vc0-lighthouse vc1-lighthouse -d

run-charon-nimbus:
	docker compose up node0 node1 vc0-nimbus vc1-nimbus -d

run-charon-lodestar:
	docker compose up node0 node1 vc0-lodestar vc1-lodestar -d

run-charon-prysm:
	docker compose up node0 node1 vc0-prysm vc1-prysm -d

run-charon-teku:
	docker compose up node0 node1 vc0-teku vc1-teku -d

clean:
	kurtosis enclave stop local-eth-testnet
	kurtosis enclave rm local-eth-testnet
	rm -rf node*
	rm -f planprint
	rm -rf keystore
	docker compose down
