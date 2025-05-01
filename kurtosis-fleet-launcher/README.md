# Kurtosis Fleet Launcher

This Python script automates the launch of a fleet of EC2 spot instances, each running a different test combination of validator, execution and consensus clients with Charon.

The launcher supports:
- Run the tests for a branch (e.g., `feat/release-test`)
- Specifying combinations of EL/CL/VC clients to test against Charon
- Launching each combination on a separate EC2 instance
- Automatic shutdown of instances after a defined lifetime

---

## Requirements

- Python 3.8+
- AWS CLI installed and configured with access to start EC2 instances

---

## Setup

Clone the repository:
```
git clone https://github.com/your-org/kurtosis-charon.git

cd kurtosis-charon/tools/fleet_launcher
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
python kurtosis_fleet_launcher.py --branch feat/release-combo-test --lifetime 60
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
--monitoring-token Obol central monitoring token
--branch           Git branch of the kurtosis-charon repo to test (default: main)
--lifetime         Shutdown time in minutes before the EC2 instance is terminated (default: 60)
--env-dir          The path to the env directory to load the combos config (default: ../deployments/env)
--terminate        Terminates the instances (it creates the instances list from the env combos directory)
```
---

## Debugging

To debug a running instance:
1. Find the instance ID in the AWS console (tagged with the test combo name).
2. SSH into the instance:
   ssh -i ~/.ssh/your-key.pem ubuntu@<public-ip>
3. Check the Docker containers logs

---
