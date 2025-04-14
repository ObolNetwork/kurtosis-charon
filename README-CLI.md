# Kurtosis Charon CLI

A CLI tool for deploying Charon clusters using Kurtosis, Kubernetes, and Helm. This tool orchestrates the entire deployment process from setting up the execution and consensus clients to deploying validators with Charon.

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
# Clone the repository
git clone https://github.com/obol/kurtosis-charon.git
cd kurtosis-charon
```

2. Build the binary:
```bash
go build -o kc main.go
```

## Usage

### Basic Deployment

```bash
./kc deploy --el geth --cl lighthouse --vc 2,2,1,1 --step 7 --skip 3
```

This command will:
- Deploy a Geth execution client
- Deploy a Lighthouse consensus client
- Deploy charon with 4 validators (2 Teku, 2 Lighthouse)
- Run all deployment steps (1-7)

### Command Options

- `--el, -e`: Execution layer client (e.g., geth, nethermind)
- `--cl, -c`: Consensus layer client (e.g., nimbus, lighthouse)
- `--vc, -v`: Validator client type encoding (e.g., 0,0,1,2 for two Teku, one Lighthouse, and one Lodestar)
- `--step`: Run steps up to this number (1-7). If not specified, runs all steps.
- `--skip`: Comma-separated list of steps to skip (e.g., 2,3)
- `--verbose`: Enable verbose logging

### Step Control

You can control the deployment process in several ways:

1. Run all steps:
```bash
kurtosis-charon deploy --el geth --cl lighthouse --vc 2,2,1,1
```

2. Run up to a specific step:
```bash
kurtosis-charon deploy --el geth --cl lighthouse --vc 2,2,1,1 --step 4
```

3. Skip specific steps (note: step 1 cannot be skipped as it is required for configuration):
```bash
kurtosis-charon deploy --el geth --cl lighthouse --vc 2,2,1,1 --skip 2,3,5
```

4. Run from a step and skip others (note: step 1 cannot be skipped):
```bash
kurtosis-charon deploy --el geth --cl lighthouse --vc 2,2,1,1 --step 4 --skip 5,6
```

### Deployment Steps

The deployment process consists of 7 steps:

1. **Cleanup and Validation** (Required - cannot be skipped)
   - Cleans up old Kubernetes namespaces
   - Removes local folders and files
   - Generates Helm values file
   - Validates configuration

2. **Kurtosis Cluster Deployment**
   - Deploy execution and consensus clients using Kurtosis
   - Store the output of the Kurtosis plan in a file
   - Get the enclave UUID
   - Download testnet folder from enclave
   - Get the genesis timestamp from the Beacon Node
   - Update the values file with the genesis timestamp

3. **Key Generation**
   - Download validator keys from the first VC instance
   - Generate new validator keys
   - Set up key storage and permissions

4. **Charon Setup**
   - Run Charon cli to create cluster configuration
   - Generate .env file

5. **S3 Upload**
   - Upload testnet configurations to S3
   - Upload Charon cluster files to S3
   - Clean up local temporary files

6. **AWS Secret Creation**
   - Create AWS secret in Kubernetes namespace

7. **Helm Deployment**
   - Create Lighthouse validator definitions if VC contains Lighthouse
   - Deploy Charon nodes using Helm
   - Delete first VC pod to prevent conflicts

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

```json
{
  "application": "kurtosis-charon",
  "version": "0.1.0",
  "enclave": "kt-geth-lighthouse-2,2,1,1",
  "timestamp": "2024-03-21T12:34:56.789Z",
  "level": "info",
  "message": "Step 1: Performing cleanup and validation"
}
```

## Development

### Prerequisites

- Go 1.21 or later
- Docker
- Kubernetes
- Helm
- AWS CLI
- Kurtosis CLI

### Building

```bash
make build
```

### Testing

```bash
make test
```

## Contributing

1. Fork the repository
2. Create a feature branch
3. Commit your changes
4. Push to the branch
5. Create a Pull Request