FROM docker:28-dind

RUN wget -O 'kurtosis.tar.gz' "https://github.com/kurtosis-tech/kurtosis-cli-release-artifacts/releases/download/1.7.2/kurtosis-cli_1.7.2_linux_amd64.tar.gz"; \
  tar --extract \
  --file kurtosis.tar.gz; \
  rm kurtosis.tar.gz; \
  mv /kurtosis /usr/local/bin/kurtosis \
  ;

RUN apk add --no-cache \
  bash \
  jq \
  envsubst \
  curl \
  dbus \
  ;

COPY . .

ENTRYPOINT ["/bin/sh", "-c" , \
  "dockerd-entrypoint.sh > /dev/null 2>&1 & \
  echo 'Waiting for docker engine to start...' && \
  sleep 10 && \
  ./run.sh && \
  echo 'Test has started!' && \
  sleep infinity"]

