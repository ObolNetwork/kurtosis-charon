# Kurtosis AWS Runner

This Python script automates the launch of a fleet of EC2 spot instances, each running a different test combination of validator, execution and consensus clients with Charon.

The launcher supports:
- Running tests for a specified branch (e.g., `feat/release-test`)
- Specifying combinations of EL/CL/VC clients to test against Charon
- Launching each combination on a separate EC2 instance
- Automatic shutdown of instances after a defined lifetime
- Optional use of On-Demand EC2 instances
- Customizable EC2 instance type per launch

---

## Requirements

- Python 3.8+
- AWS CLI installed and configured with access to start EC2 instances

---

## Setup

Clone the repository:
```
git clone https://github.com/ObolNetwork/kurtosis-charon.git

cd kurtosis-charon/kurtosis-aws-runner
```
Create and activate a Python virtual environment:
```
python3 -m venv venv

source venv/bin/activate
```
Install dependencies:
```
pip install -r requirements.txt
```
Ensure your AWS credentials are set.

---

## Usage

Run the launcher with the following command:
```
python kurtosis_aws_runner.py --branch feat/release-combo-test --lifetime 60 --monitoring-token <YOUR_TOKEN>
```
This will:
- Find all EL/CL/VC combinations (e.g., Teku/Lodestar, Lighthouse/Nimbus)
- Prompt you to confirm before launching
- Start one EC2 instance per combination with a shutdown timer of 60 minutes

```
Example prompt:
🔍 Found 2 combinations:
  - geth-lighthouse-charon-lodestar
  - geth-teku-charon-lodestar

Proceed to launch 2 EC2 instances? [y/N]:
```
---

## Configuration

The following flags are supported:
```
--monitoring-token  Obol central monitoring token (required)
--branch            Git branch of the kurtosis-charon repo to test (default: main)
--lifetime          Shutdown time in minutes before the EC2 instance is terminated (default: 60)
--env-dir           The path to the env directory to load the combos config (default: ../deployments/env)
--terminate         Terminates the instances (uses combos from the env directory to find instances)
--on-demand         Use On-Demand EC2 instances instead of Spot instances
--instance-type     EC2 instance type to launch (default: c6a.xlarge)
```

**Note**: The default EBS volume size is 50 GB.

---

## Debugging

To debug a running instance:
1. Find the instance ID in the AWS console (tagged with the test combo name).
2. SSH into the instance:
   ```
   ssh -i ~/.ssh/your-key.pem ubuntu@<public-ip>
   ```
3. Check the Docker container logs:
   ```
   docker ps
   docker logs <container-id>
   ```
---
