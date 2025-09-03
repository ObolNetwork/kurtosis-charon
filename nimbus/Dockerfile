ARG VERSION

FROM docker.ethquokkaops.io/dh/ethpandaops/nimbus-eth2:${VERSION} AS nimbusbn

FROM docker.ethquokkaops.io/dh/ethpandaops/nimbus-validator-client:${VERSION}

COPY --from=nimbusbn /home/user/nimbus_beacon_node /home/user/nimbus_beacon_node

ENTRYPOINT ["/home/user/data/run.sh"]
