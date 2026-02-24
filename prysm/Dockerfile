ARG VC_IMAGE
ARG VC_VERSION

FROM ${VC_IMAGE}:${VC_VERSION} AS prysm-validator

FROM debian:buster-slim
COPY --from=prysm-validator /app/cmd/validator/validator validator

ENTRYPOINT [ "/home/prysm/run.sh" ]