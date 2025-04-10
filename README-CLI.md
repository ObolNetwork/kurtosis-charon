# Kurtosis Charon CLI

A CLI tool for deploying Charon validator clusters using Kurtosis, Kubernetes, and Helm. This tool orchestrates the entire deployment process from setting up execution and consensus clients to deploying validators with Charon.

## Features

- **Multi-Client Support**
  - Execution Layer: Geth, Nethermind
  - Consensus Layer: Nimbus, Lighthouse, Lodestar, Prysm, Teku
  - Validator Clients: Teku (0), Lighthouse (1), Lodestar (2), Nimbus (3), Prysm (4)

- **Automated Deployment**
  - Step-by-step deployment process (1-7)
  - Automated S3 configuration storage
  - Helm-based deployment with dynamic values generation
  - Comprehensive logging for monitoring and debugging

- **Logging and Monitoring**
  - Structured JSON logging
  - Loki-compatible log format
  - Enclave-specific log files
  - Detailed deployment tracking

## Prerequisites

- Go 1.16 or later
- Kubernetes cluster
- Helm
- AWS CLI configured
- Kurtosis CLI
- Docker
- Configure Kurtosis 
```bash
kurtosis config path
cat kurtosis/kurtosis-config.yaml
in the config set - 
  cloud:
    type: "kubernetes"
    config:
      kubernetes-cluster-name: obol-ams2-1
      storage-class: "openebs-hostpath"
      enclave-size-in-megabytes: 1000000
kurtosis engine restart
kurtosis cluster set cloud
```

## Installation

1. Clone the repository:
```bash
git clone <repository-url>
cd kurtosis-charon
```

2. Build the binary:
```bash
go build -o kc main.go
```

## Usage

### Basic Deployment

```bash
./kc deploy --el geth --cl lighthouse --vc 2,2,1,1 --step 7
```

This command will:
- Deploy a Geth execution client
- Deploy a Lighthouse consensus client
- Deploy charon with 4 validators (2 Teku, 2 Lighthouse)
- Run all deployment steps (1-7)

### Command Options

- `--el, -e`: Execution layer client (geth, nethermind)
- `--cl, -c`: Consensus layer client (nimbus, lighthouse, lodestar, prysm, teku)
- `--vc, -v`: Validator client type encoding (e.g., 0,0,1,2 for two Teku, one Lighthouse, and one Lodestar)
- `--step`: Run steps up to this number (1-7). If not specified, runs all steps.
- `--verbose`: Enable verbose logging

### Validator Client Types

- `0`: Teku
- `1`: Lighthouse
- `2`: Lodestar
- `3`: Nimbus
- `4`: Prysm

## Deployment Steps

1. **Environment Setup**
   - Initialize Kurtosis engine
   - Start Kurtosis gateway
   - Configure AWS credentials

2. **Cluster Creation**
   - Create Kubernetes namespace
   - Deploy execution and consensus clients
   - Set up validator clients

3. **Charon Configuration**
   - Generate cluster configuration
   - Create validator keys
   - Set up S3 storage

4. **Helm Deployment**
   - Deploy Charon nodes
   - Configure validator clients
   - Set up monitoring

## Logging

The application uses structured logging with the following features:

- Logs are written to both stdout and enclave-specific log files
- JSON format for better integration with log aggregation systems
- Common fields for log correlation:
  - `application`: "kurtosis-charon"
  - `version`: Current version
  - `enclave`: Enclave name (e.g., kt-geth-lighthouse-2,2,1,1)
  - `timestamp`: RFC3339Nano format
  - `level`: Log level
  - `message`: Log message

### Log File Location

Log files are created in the format:
```
kurtosis-charon-kt-<el>-<cl>-<vc>.log
```

Example:
```
kurtosis-charon-kt-geth-lighthouse-2,2,1,1.log
```

## Development

### Building

```bash
go build -o kc main.go
```

### Testing

```bash
go test ./...
```

## Contributing

1. Fork the repository
2. Create a feature branch
3. Commit your changes
4. Push to the branch
5. Create a Pull Request