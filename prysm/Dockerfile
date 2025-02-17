FROM gcr.io/prysmaticlabs/prysm/validator:v5.3.0 as prysm-validator

FROM debian:buster-slim
COPY --from=prysm-validator /app/cmd/validator/validator validator

ENTRYPOINT [ "/home/prysm/run.sh" ]