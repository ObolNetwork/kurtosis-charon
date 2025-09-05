ARG VERSION

FROM docker.ethquokkaops.io/dh/ethpandaops/nimbus-eth2:${VERSION} AS nimbusbn

FROM docker.ethquokkaops.io/dh/ethpandaops/nimbus-validator-client:${VERSION}

COPY --from=nimbusbn /home/user/nimbus-eth2/build/nimbus_beacon_node /home/user/nimbus-eth2/build/nimbus_beacon_node

ENTRYPOINT ["/home/user/data/run.sh"]
