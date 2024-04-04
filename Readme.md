# Kurtosis-Charon setup

## TL;DR
```bash
make geth-lightouse
make charon
make run-charon-lodestar
```

## Setup and Run

Install kurtosis-cli by following the instructions [here](https://docs.kurtosis.com/install).
Ensure you have docker installed on your machine.
To set up and run the project, follow these steps:
Below example runs a geth-lighthouse-charon-teku setup.
Follow the ```Makefile``` to run other combinations.

Step 1: Run the following command to run geth-lighthouse setup:
    
    ```bash
    make geth-lightouse
    ```
Step 2: Verify that the setup is running and wait for 10 seconds. Create a Charon setup by running the following command:
    
    ```bash
    make charon
    ```

Step 3: Verify the ```.env``` file. Next run the following command to start the validator client:
    
    ```bash
    make run-charon-lightouse
    ```

## Tear Down

To tear down the project, run the following command:

    ```bash
    make clean
    ```