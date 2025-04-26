.PHONY: run geth-lighthouse geth-nimbus geth-lodestar geth-prysm geth-teku charon run-charon-lighthouse run-charon-nimbus run-charon-lodestar run-charon-prysm run-charon-teku clean

# Define the composite step
geth-lighthouse-charon-lighthouse: geth-lighthouse charon run-charon-lighthouse
geth-lighthouse-charon-lodestar: geth-lighthouse charon run-charon-lodestar
geth-lighthouse-charon-teku: geth-lighthouse charon run-charon-teku
geth-lighthouse-charon-nimbus: geth-lighthouse charon run-charon-nimbus
geth-lighthouse-charon-prysm: geth-lighthouse charon run-charon-prysm

geth-lodestar-charon-lighthouse: geth-lodestar charon run-charon-lighthouse
geth-lodestar-charon-lodestar: geth-lodestar charon run-charon-lodestar
geth-lodestar-charon-teku: geth-lodestar charon run-charon-teku
geth-lodestar-charon-nimbus: geth-lodestar charon run-charon-nimbus
geth-lodestar-charon-prysm: geth-lodestar charon run-charon-prysm

geth-teku-charon-lighthouse: geth-teku charon run-charon-lighthouse
geth-teku-charon-lodestar: geth-teku charon run-charon-lodestar
geth-teku-charon-teku: geth-teku charon run-charon-teku
geth-teku-charon-nimbus: geth-teku charon run-charon-nimbus
geth-teku-charon-prysm: geth-teku charon run-charon-prysm

geth-nimbus-charon-lighthouse: geth-nimbus charon run-charon-lighthouse
geth-nimbus-charon-lodestar: geth-nimbus charon run-charon-lodestar
geth-nimbus-charon-teku: geth-nimbus charon run-charon-teku
geth-nimbus-charon-nimbus: geth-nimbus charon run-charon-nimbus
geth-nimbus-charon-prysm: geth-nimbus charon run-charon-prysm

geth-prysm-charon-lighthouse: geth-prysm charon run-charon-lighthouse
geth-prysm-charon-lodestar: geth-prysm charon run-charon-lodestar
geth-prysm-charon-teku: geth-prysm charon run-charon-teku
geth-prysm-charon-nimbus: geth-prysm charon run-charon-nimbus
geth-prysm-charon-prysm: geth-prysm charon run-charon-prysm

geth-grandine-charon-lighthouse: geth-grandine charon run-charon-lighthouse
geth-grandine-charon-lodestar: geth-grandine charon run-charon-lodestar
geth-grandine-charon-teku: geth-grandine charon run-charon-teku
geth-grandine-charon-nimbus: geth-grandine charon run-charon-nimbus
geth-grandine-charon-prysm: geth-grandine charon run-charon-prysm

geth-lighthouse:
	rm -f planprint
	CL_TYPE=lighthouse ./setup_el_cl.sh
	kurtosis run --enclave local-eth-testnet github.com/ethpandaops/ethereum-package --args-file ./network_params.yaml > planprint
	@echo "Waiting for 10 seconds..."
	@sleep 10

geth-nimbus:
	@echo "WARNING: Nimbus BN requires Charon to enable feature json_requests"
	@read -p "Press enter to continue"
	rm -f planprint
	CL_TYPE=nimbus ./setup_el_cl.sh
	kurtosis run --enclave local-eth-testnet github.com/ethpandaops/ethereum-package --args-file ./network_params.yaml > planprint
	@echo "Waiting for 10 seconds..."
	@sleep 10

geth-lodestar:
	rm -f planprint
	CL_TYPE=lodestar ./setup_el_cl.sh
	kurtosis run --enclave local-eth-testnet github.com/ethpandaops/ethereum-package --args-file ./network_params.yaml > planprint
	@echo "Waiting for 10 seconds..."
	@sleep 10

geth-prysm:
	rm -f planprint
	CL_TYPE=prysm ./setup_el_cl.sh
	kurtosis run --enclave local-eth-testnet github.com/ethpandaops/ethereum-package --args-file ./network_params.yaml > planprint
	@echo "Waiting for 60 seconds... don't skip the wait"
	@sleep 60

geth-teku:
	rm -f planprint
	CL_TYPE=teku ./setup_el_cl.sh
	kurtosis run --enclave local-eth-testnet github.com/ethpandaops/ethereum-package --args-file ./network_params.yaml > planprint
	@echo "Waiting for 60 seconds... don't skip the wait"
	@sleep 60

geth-grandine:
	rm -f planprint
	CL_TYPE=grandine ./setup_el_cl.sh
	kurtosis run --enclave local-eth-testnet github.com/ethpandaops/ethereum-package --args-file ./network_params.yaml > planprint
	@echo "Waiting for 10 seconds... don't skip the wait"
	@sleep 10

charon:
	./setup_charon.sh
	./setup_monitoring.sh

run-charon-lighthouse:
	VC_TYPE=lighthouse ./setup_vc.sh
	docker compose --env-file ".env" -f ./compose.charon.yaml -f ./compose.lighthouse.yaml up -d

run-charon-nimbus:
	VC_TYPE=nimbus ./setup_vc.sh
	docker compose --env-file ".env" -f ./compose.charon.yaml -f ./compose.nimbus.yaml up -d

run-charon-lodestar:
	VC_TYPE=lodestar ./setup_vc.sh
	docker compose --env-file ".env" -f ./compose.charon.yaml -f ./compose.lodestar.yaml up -d

run-charon-prysm:
	VC_TYPE=prysm ./setup_vc.sh
	docker compose --env-file ".env" -f ./compose.charon.yaml -f ./compose.prysm.yaml up -d

run-charon-teku:
	VC_TYPE=teku ./setup_vc.sh
	docker compose --env-file ".env" -f ./compose.charon.yaml -f ./compose.teku.yaml up -d

exit-lighthouse:
	./lighthouse/exit.sh 0
	./lighthouse/exit.sh 1
	./lighthouse/exit.sh 2

exit-nimbus:
	./nimbus/exit.sh 0
	./nimbus/exit.sh 1
	./nimbus/exit.sh 2

exit-lodestar:
	./lodestar/exit.sh 0
	./lodestar/exit.sh 1
	./lodestar/exit.sh 2

exit-teku:
	./teku/exit.sh 0
	./teku/exit.sh 1
	./teku/exit.sh 2

clean:
	-docker compose down
	-kurtosis enclave stop local-eth-testnet
	-kurtosis enclave rm local-eth-testnet
	rm -rf node*
	rm -f planprint
	rm -rf keystore
	rm -rf testnet
	rm -rf charon-keystore
	rm -rf charon-keys
	rm -rf .charon
	rm -rf data
	rm -rf network_params.yaml
	rm -f .env
