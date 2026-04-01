ARG VC_IMAGE
ARG VC_VERSION
ARG NIMBUS_BN_IMAGE

FROM ${NIMBUS_BN_IMAGE}:${VC_VERSION} AS nimbusbn

FROM ${VC_IMAGE}:${VC_VERSION}

COPY --from=nimbusbn /home/user/nimbus_beacon_node /home/user/nimbus_beacon_node

ENTRYPOINT ["/home/user/data/run.sh"]
