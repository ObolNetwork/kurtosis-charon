FROM statusim/nimbus-eth2:multiarch-v24.5.1 as nimbusbn

FROM statusim/nimbus-validator-client:multiarch-v24.5.1

COPY --from=nimbusbn /home/user/nimbus_beacon_node /home/user/nimbus_beacon_node

ENTRYPOINT ["/home/user/data/run.sh"]
