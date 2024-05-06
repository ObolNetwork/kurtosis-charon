# Kurtosis-Charon setup
## Pre-requisites

Install kurtosis-cli by following the instructions [here](https://docs.kurtosis.com/install).
Ensure you have docker installed on your machine.

MAKE SURE `kurtosis-cli` IS THE LATEST VERSION.

```shell

## TL;DR

```shell
make geth-lighthouse
make charon
make run-charon-lodestar
```

## Setup and Run

To set up and run the project, follow these steps:
Below example runs a geth-lighthouse-charon-lodestar setup.
Follow the `Makefile` to run other combinations.

Step 1: Run the following command to run geth-lighthouse setup:
    
```shell
make geth-lighthouse
```

Step 2: Verify that the setup is running in your docker-desktop and wait for 10 seconds. Create a Charon setup by running the following command:
    
```shell
make charon
```

Step 3: Verify the `.env` file. Next run the following command to start the validator client:
    
```shell
make run-charon-lodestar
```

## Tear Down

To tear down the project, run the following command:

```shell
make clean
```

## Troubleshooting
If the project setup fails or hangs, try the following:

    ```
    restart docker-desktop manually
    ```
    
    ```
    delete all running containers
    ```
    
Clean kurtosis setup by running the following command:

```shell
kurtosis clean
```

## Note

The docker compose and the `run_charon.sh` use `obolnetwork/charon-local:latest` (local build). 
Change to other versions in those two places depending on your testing needs.
