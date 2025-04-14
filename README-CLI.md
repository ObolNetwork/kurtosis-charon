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

- Go 1.21 or later
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

### Prerequisites
- Go 1.21 or later
- Docker and Docker Compose
- Kurtosis CLI
- Make (for Make-based deployment)

### Building the CLI
```bash
# Build the binary
make build
```

### Running the Deployment

There are two ways to run the deployment:

#### 1. Using the kc Binary Directly
```bash
# Basic deployment
./kc deploy --el <execution-layer> --cl <consensus-layer> --vc <validator-counts>

# Example: Deploy Geth with Lighthouse and 2,2,4,4 validators
./kc deploy --el geth --cl lighthouse --vc 2,2,4,4

# Skip specific steps
./kc deploy --el geth --cl lighthouse --vc 2,2,4,4 --skip 1,2,3,4,5,6

# Run only specific step
./kc deploy --el geth --cl lighthouse --vc 2,2,4,4 --step 7
```

#### 2. Using Make
```bash
# Basic deployment
make deploy-k8s el=<execution-layer> cl=<consensus-layer> vc=<validator-counts>

# Example: Deploy Geth with Lighthouse and 2,2,4,4 validators
make deploy-k8s el=geth cl=lighthouse vc=2,2,4,4

# Skip specific steps
make deploy-k8s el=geth cl=lighthouse vc=2,2,4,4 skip=1,2,3,4,5,6

# Run only specific step
make deploy-k8s el=geth cl=lighthouse vc=2,2,4,4 step=7

# Skip steps and run specific step
make deploy-k8s el=geth cl=lighthouse vc=2,2,4,4 step=7 skip=1,2,3,4,5,6

# Run all 25 combinations of CL and VC deployments
make deploy-k8s-all
```

### Available Options

- `--el` or `el`: Execution Layer Client (geth, nethermind, besu)
- `--cl` or `cl`: Consensus Layer Client (lighthouse, lodestar, nimbus, prysm, teku)
- `--vc` or `vc`: Validator Counts (comma-separated list, e.g., "2,2,4,4")
- `--skip` or `skip`: Steps to skip (comma-separated list, e.g., "1,2,3")
- `--step` or `step`: Specific step to run (1-7)

### Deployment Combinations

The tool supports all possible combinations of Consensus Layer (CL) and Validator Client (VC) deployments:

1. **Consensus Layer Clients (5)**
   - Lighthouse
   - Lodestar
   - Nimbus
   - Teku
   - Prysm

2. **Validator Client Types (5)**
   - 0: Teku
   - 1: Lighthouse
   - 2: Lodestar
   - 3: Nimbus
   - 4: Prysm

3. **Combination Examples**
   Single VC type:
   - Lighthouse CL with all Lighthouse VCs: `make run-deployment el=geth cl=lighthouse vc=1,1,1,1`
   - Lodestar CL with all Lodestar VCs: `make run-deployment el=geth cl=lodestar vc=2,2,2,2`
   - Nimbus CL with all Nimbus VCs: `make run-deployment el=geth cl=nimbus vc=3,3,3,3`
   - Teku CL with all Teku VCs: `make run-deployment el=geth cl=teku vc=0,0,0,0`
   - Prysm CL with all Prysm VCs: `make run-deployment el=geth cl=prysm vc=4,4,4,4`

   Multiple VC types:
   - Lighthouse CL with all Lighthouse VCs: `make run-deployment el=geth cl=lighthouse vc=1,1,2,2`
   - Lodestar CL with all Lodestar VCs: `make run-deployment el=geth cl=lodestar vc=2,2,3,3`
   - etc

To run all 25 possible combinations in sequence, use:
```bash
make deploy-k8s-all
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