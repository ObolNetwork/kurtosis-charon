ARG VERSION

FROM docker.ethquokkaops.io/dh/ethpandaops/prysm-validator:${VERSION} AS prysm-validator

FROM debian:buster-slim
COPY --from=prysm-validator /app/cmd/validator/validator validator

ENTRYPOINT [ "/home/prysm/run.sh" ]