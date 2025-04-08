FROM kaloyantanev/prysm:latest as prysm-validator

FROM debian:buster-slim
RUN apt-get update && apt-get install -y \
    bash \
    && rm -rf /var/lib/apt/lists/*

COPY --from=prysm-validator /app/cmd/validator/validator /validator
COPY run.sh /run.sh
RUN chmod +x /run.sh

ENTRYPOINT [ "/run.sh" ]