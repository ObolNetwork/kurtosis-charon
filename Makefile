.PHONY: run geth-lighthouse geth-nimbus geth-lodestar geth-prysm geth-teku geth-grandine charon run-charon-lighthouse run-charon-nimbus run-charon-lodestar run-charon-prysm run-charon-teku run-charon-vouch run-aws stop-aws clean

# Define the composite step
# Each target sets CLUSTER_NAME=kurtosis-{cl}-{vc} for metrics identification.
geth-lighthouse-charon-lighthouse:
	$(MAKE) geth-lighthouse
	CLUSTER_NAME=kurtosis-lighthouse-lighthouse $(MAKE) charon
	$(MAKE) run-charon-lighthouse
geth-lighthouse-charon-lodestar:
	$(MAKE) geth-lighthouse
	CLUSTER_NAME=kurtosis-lighthouse-lodestar $(MAKE) charon
	$(MAKE) run-charon-lodestar
geth-lighthouse-charon-teku:
	$(MAKE) geth-lighthouse
	CLUSTER_NAME=kurtosis-lighthouse-teku $(MAKE) charon
	$(MAKE) run-charon-teku
geth-lighthouse-charon-nimbus:
	$(MAKE) geth-lighthouse
	CLUSTER_NAME=kurtosis-lighthouse-nimbus $(MAKE) charon
	$(MAKE) run-charon-nimbus
geth-lighthouse-charon-prysm:
	$(MAKE) geth-lighthouse
	CLUSTER_NAME=kurtosis-lighthouse-prysm $(MAKE) charon
	$(MAKE) run-charon-prysm
geth-lighthouse-charon-vouch:
	$(MAKE) geth-lighthouse
	CLUSTER_NAME=kurtosis-lighthouse-vouch $(MAKE) charon
	$(MAKE) run-charon-vouch

geth-lodestar-charon-lighthouse:
	$(MAKE) geth-lodestar
	CLUSTER_NAME=kurtosis-lodestar-lighthouse $(MAKE) charon
	$(MAKE) run-charon-lighthouse
geth-lodestar-charon-lodestar:
	$(MAKE) geth-lodestar
	CLUSTER_NAME=kurtosis-lodestar-lodestar $(MAKE) charon
	$(MAKE) run-charon-lodestar
geth-lodestar-charon-teku:
	$(MAKE) geth-lodestar
	CLUSTER_NAME=kurtosis-lodestar-teku $(MAKE) charon
	$(MAKE) run-charon-teku
geth-lodestar-charon-nimbus:
	$(MAKE) geth-lodestar
	CLUSTER_NAME=kurtosis-lodestar-nimbus $(MAKE) charon
	$(MAKE) run-charon-nimbus
geth-lodestar-charon-prysm:
	$(MAKE) geth-lodestar
	CLUSTER_NAME=kurtosis-lodestar-prysm $(MAKE) charon
	$(MAKE) run-charon-prysm
geth-lodestar-charon-vouch:
	$(MAKE) geth-lodestar
	CLUSTER_NAME=kurtosis-lodestar-vouch $(MAKE) charon
	$(MAKE) run-charon-vouch

geth-teku-charon-lighthouse:
	$(MAKE) geth-teku
	CLUSTER_NAME=kurtosis-teku-lighthouse $(MAKE) charon
	$(MAKE) run-charon-lighthouse
geth-teku-charon-lodestar:
	$(MAKE) geth-teku
	CLUSTER_NAME=kurtosis-teku-lodestar $(MAKE) charon
	$(MAKE) run-charon-lodestar
geth-teku-charon-teku:
	$(MAKE) geth-teku
	CLUSTER_NAME=kurtosis-teku-teku $(MAKE) charon
	$(MAKE) run-charon-teku
geth-teku-charon-nimbus:
	$(MAKE) geth-teku
	CLUSTER_NAME=kurtosis-teku-nimbus $(MAKE) charon
	$(MAKE) run-charon-nimbus
geth-teku-charon-prysm:
	$(MAKE) geth-teku
	CLUSTER_NAME=kurtosis-teku-prysm $(MAKE) charon
	$(MAKE) run-charon-prysm
geth-teku-charon-vouch:
	$(MAKE) geth-teku
	CLUSTER_NAME=kurtosis-teku-vouch $(MAKE) charon
	$(MAKE) run-charon-vouch

geth-nimbus-charon-lighthouse:
	$(MAKE) geth-nimbus
	CLUSTER_NAME=kurtosis-nimbus-lighthouse $(MAKE) charon
	$(MAKE) run-charon-lighthouse
geth-nimbus-charon-lodestar:
	$(MAKE) geth-nimbus
	CLUSTER_NAME=kurtosis-nimbus-lodestar $(MAKE) charon
	$(MAKE) run-charon-lodestar
geth-nimbus-charon-teku:
	$(MAKE) geth-nimbus
	CLUSTER_NAME=kurtosis-nimbus-teku $(MAKE) charon
	$(MAKE) run-charon-teku
geth-nimbus-charon-nimbus:
	$(MAKE) geth-nimbus
	CLUSTER_NAME=kurtosis-nimbus-nimbus $(MAKE) charon
	$(MAKE) run-charon-nimbus
geth-nimbus-charon-prysm:
	$(MAKE) geth-nimbus
	CLUSTER_NAME=kurtosis-nimbus-prysm $(MAKE) charon
	$(MAKE) run-charon-prysm
geth-nimbus-charon-vouch:
	$(MAKE) geth-nimbus
	CLUSTER_NAME=kurtosis-nimbus-vouch $(MAKE) charon
	$(MAKE) run-charon-vouch

geth-prysm-charon-lighthouse:
	$(MAKE) geth-prysm
	CLUSTER_NAME=kurtosis-prysm-lighthouse $(MAKE) charon
	$(MAKE) run-charon-lighthouse
geth-prysm-charon-lodestar:
	$(MAKE) geth-prysm
	CLUSTER_NAME=kurtosis-prysm-lodestar $(MAKE) charon
	$(MAKE) run-charon-lodestar
geth-prysm-charon-teku:
	$(MAKE) geth-prysm
	CLUSTER_NAME=kurtosis-prysm-teku $(MAKE) charon
	$(MAKE) run-charon-teku
geth-prysm-charon-nimbus:
	$(MAKE) geth-prysm
	CLUSTER_NAME=kurtosis-prysm-nimbus $(MAKE) charon
	$(MAKE) run-charon-nimbus
geth-prysm-charon-prysm:
	$(MAKE) geth-prysm
	CLUSTER_NAME=kurtosis-prysm-prysm $(MAKE) charon
	$(MAKE) run-charon-prysm
geth-prysm-charon-vouch:
	$(MAKE) geth-prysm
	CLUSTER_NAME=kurtosis-prysm-vouch $(MAKE) charon
	$(MAKE) run-charon-vouch

geth-grandine-charon-lighthouse:
	$(MAKE) geth-grandine
	CLUSTER_NAME=kurtosis-grandine-lighthouse $(MAKE) charon
	$(MAKE) run-charon-lighthouse
geth-grandine-charon-lodestar:
	$(MAKE) geth-grandine
	CLUSTER_NAME=kurtosis-grandine-lodestar $(MAKE) charon
	$(MAKE) run-charon-lodestar
geth-grandine-charon-teku:
	$(MAKE) geth-grandine
	CLUSTER_NAME=kurtosis-grandine-teku $(MAKE) charon
	$(MAKE) run-charon-teku
geth-grandine-charon-nimbus:
	$(MAKE) geth-grandine
	CLUSTER_NAME=kurtosis-grandine-nimbus $(MAKE) charon
	$(MAKE) run-charon-nimbus
geth-grandine-charon-prysm:
	$(MAKE) geth-grandine
	CLUSTER_NAME=kurtosis-grandine-prysm $(MAKE) charon
	$(MAKE) run-charon-prysm
geth-grandine-charon-vouch:
	$(MAKE) geth-grandine
	CLUSTER_NAME=kurtosis-grandine-vouch $(MAKE) charon
	$(MAKE) run-charon-vouch

geth-lighthouse:
	CL_TYPE=lighthouse ./setup_el_cl.sh
	kurtosis run --enclave local-eth-testnet github.com/ethpandaops/ethereum-package --args-file ./network_params.yaml > planprint
	@echo "Waiting for 10 seconds..."
	@sleep 10

geth-nimbus:
	CL_TYPE=nimbus ./setup_el_cl.sh
	kurtosis run --enclave local-eth-testnet github.com/ethpandaops/ethereum-package --args-file ./network_params.yaml > planprint
	@echo "Waiting for 10 seconds..."
	@sleep 10

geth-lodestar:
	CL_TYPE=lodestar ./setup_el_cl.sh
	kurtosis run --enclave local-eth-testnet github.com/ethpandaops/ethereum-package --args-file ./network_params.yaml > planprint
	@echo "Waiting for 10 seconds..."
	@sleep 10

geth-prysm:
	CL_TYPE=prysm ./setup_el_cl.sh
	kurtosis run --enclave local-eth-testnet github.com/ethpandaops/ethereum-package --args-file ./network_params.yaml > planprint
	@echo "Waiting for 60 seconds... don't skip the wait"
	@sleep 60

geth-teku:
	CL_TYPE=teku ./setup_el_cl.sh
	kurtosis run --enclave local-eth-testnet github.com/ethpandaops/ethereum-package --args-file ./network_params.yaml > planprint
	@echo "Waiting for 60 seconds... don't skip the wait"
	@sleep 60

geth-grandine:
	CL_TYPE=grandine ./setup_el_cl.sh
	kurtosis run --enclave local-eth-testnet github.com/ethpandaops/ethereum-package --args-file ./network_params.yaml > planprint
	@echo "Waiting for 60 seconds... don't skip the wait"
	@sleep 60

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

run-charon-vouch:
	VC_TYPE=vouch ./setup_vc.sh
	docker compose --env-file ".env" -f ./compose.charon.yaml -f ./compose.vouch.yaml up -d

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

run-aws:
	./run_aws.sh

stop-aws:
	./stop_aws.sh

clean:
	-docker compose -f compose.charon.yaml -f compose.lighthouse.yaml -f compose.lodestar.yaml -f compose.nimbus.yaml -f compose.prysm.yaml -f compose.teku.yaml down
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
