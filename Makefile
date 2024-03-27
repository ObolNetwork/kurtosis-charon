.PHONY: run clean

run:
	rm -f planprint
	kurtosis run --enclave local-eth-testnet github.com/kurtosis-tech/ethereum-package --args-file ./network_params.yaml > planprint
	./run_charon.sh

clean:
	kurtosis enclave stop local-eth-testnet
	kurtosis enclave rm local-eth-testnet
	rm -rf node*
	rm -f planprint
	rm -rf keystore
	docker compose down
