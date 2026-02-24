ARG CL_IMAGE
ARG CL_VERSION
ARG VC_IMAGE
ARG VC_VERSION

FROM ${CL_IMAGE}:${CL_VERSION} AS nimbusbn

FROM ${VC_IMAGE}:${VC_VERSION}

COPY --from=nimbusbn /home/user/nimbus_beacon_node /home/user/nimbus_beacon_node

ENTRYPOINT ["/home/user/data/run.sh"]
