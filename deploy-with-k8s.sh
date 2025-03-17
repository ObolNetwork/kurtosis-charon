#!/bin/bash

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# AWS credentials configuration
AWS_CREDENTIALS_CONFIG() {
    # Load from environment variables
    if [ -f .env ]; then
        source .env
    else
        echo -e "${RED}Warning: .env file not found. Please create one from .env.template${NC}"
        exit 1
    fi

    # Verify credentials exist
    if [ -z "$AWS_ACCESS_KEY_ID" ] || [ -z "$AWS_SECRET_ACCESS_KEY" ]; then
        echo -e "${RED}Error: AWS credentials not found in .env file${NC}"
        exit 1
    fi
}

# Load AWS credentials
AWS_CREDENTIALS_CONFIG

# Function to print usage
print_usage() {
    echo -e "${YELLOW}Usage:${NC} $0 [OPTIONS]"
    echo "Options:"
    echo "  -c, --consensus     Consensus client (lodestar, lighthouse, prysm, teku, nimbus)"
    echo "  -v, --validator     Validator client (lodestar, lighthouse, prysm, teku, nimbus)"
    echo "  -h, --help         Show this help message"
    exit 1
}

# Function to validate client names
validate_client() {
    local client=$1
    local valid_clients=("lodestar" "lighthouse" "prysm" "teku" "nimbus")
    
    for valid_client in "${valid_clients[@]}"; do
        if [ "$client" == "$valid_client" ]; then
            return 0
        fi
    done
    
    echo -e "${RED}Error: Invalid client '$client'. Must be one of: ${valid_clients[*]}${NC}"
    exit 1
}

# Function to get client abbreviation
get_client_abbrev() {
    local client=$1
    case $client in
        "lodestar")  echo "lo";;
        "lighthouse") echo "li";;
        "prysm")     echo "pr";;
        "teku")      echo "te";;
        "nimbus")    echo "ni";;
        *)           echo "unknown";;
    esac
}

# Function to cleanup existing resources
cleanup_resources() {
    local ENCLAVE_NAME=$1
    local NAMESPACE=$2
    
    echo -e "${YELLOW}Cleaning up existing resources...${NC}"
    
    # Delete namespace if it exists
    if kubectl get namespace "${NAMESPACE}" >/dev/null 2>&1; then
        echo "Deleting existing namespace ${NAMESPACE}..."
        kubectl delete namespace "${NAMESPACE}"
        # Wait for namespace deletion
        while kubectl get namespace "${NAMESPACE}" >/dev/null 2>&1; do
            echo "Waiting for namespace deletion..."
            sleep 5
        done
    fi
    
    # Delete S3 folders if they exist
    echo "Cleaning up S3 folders..."
    aws s3 rm "s3://charon-clusters-config/${NAMESPACE}" --recursive || true
    
    # Delete local enclave folder if it exists
    if [ -d "${ENCLAVE_NAME}" ]; then
        echo "Deleting local enclave folder ${ENCLAVE_NAME}..."
        rm -rf "${ENCLAVE_NAME}"
    fi
    
    # Delete local .env file if it exists
    if [ -f ".env" ]; then
        echo "Deleting local .env file..."
        rm -f ".env"
    fi
    
    echo -e "${GREEN}Cleanup completed${NC}"
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -c|--consensus)
            CONSENSUS_CLIENT="$2"
            shift 2
            ;;
        -v|--validator)
            VALIDATOR_CLIENT="$2"
            shift 2
            ;;
        -h|--help)
            print_usage
            ;;
        *)
            echo -e "${RED}Error: Unknown option $1${NC}"
            print_usage
            ;;
    esac
done

# Validate required arguments
if [ -z "$CONSENSUS_CLIENT" ] || [ -z "$VALIDATOR_CLIENT" ]; then
    echo -e "${RED}Error: Both consensus and validator clients must be specified${NC}"
    print_usage
fi

# Validate client names
validate_client "$CONSENSUS_CLIENT"
validate_client "$VALIDATOR_CLIENT"

# Get client abbreviations
CL_ABBREV=$(get_client_abbrev "$CONSENSUS_CLIENT")
VC_ABBREV=$(get_client_abbrev "$VALIDATOR_CLIENT")

# Set up variables
ENCLAVE_NAME="kurtosis-geth-${CONSENSUS_CLIENT}-${VALIDATOR_CLIENT}"
NAMESPACE="kt-${ENCLAVE_NAME}"
VALUES_FILE="${CL_ABBREV}-${VC_ABBREV}-values.yaml"
HELM_RELEASE="ch-geth-${CL_ABBREV}-${VC_ABBREV}-cluster"

echo -e "${GREEN}Starting deployment process for ${ENCLAVE_NAME}...${NC}"

# Clean up existing resources
cleanup_resources "${ENCLAVE_NAME}" "${NAMESPACE}"

# Step 1: Deploy Kurtosis cluster
echo -e "${YELLOW}Step 1: Deploying Kurtosis cluster...${NC}"
kurtosis run --enclave "${ENCLAVE_NAME}" github.com/ethpandaops/ethereum-package --args-file ./network_params_geth_${CONSENSUS_CLIENT}.yaml > "planprint-${ENCLAVE_NAME}"
echo "Waiting for 60 seconds..."
sleep 60
echo "Done"

# # Step 1a: Resize PVC
# echo -e "${YELLOW}Step 1a: Resizing PVC to 100Gi...${NC}"
# PVC_NAME="enclave-data"

# # Get PV name
# PV_NAME=$(kubectl get pvc ${PVC_NAME} -n ${NAMESPACE} -o jsonpath='{.spec.volumeName}')
# echo "Found PV: ${PV_NAME}"

# # Create a temporary file with the PV configuration
# TMP_FILE=$(mktemp)
# kubectl get pv ${PV_NAME} -o yaml > ${TMP_FILE}

# # Show current configuration
# echo "Current PV configuration:"
# cat ${TMP_FILE}

# # Update the storage size in the PV YAML
# sed -i '' 's/storage: .*/storage: 100Gi/' ${TMP_FILE}

# echo "Modified PV configuration:"
# cat ${TMP_FILE}

# # Apply the modified PV configuration
# echo "Applying updated PV configuration..."
# if ! kubectl apply -f ${TMP_FILE}; then
#     echo -e "${RED}Failed to apply PV configuration${NC}"
#     echo "Trying with kubectl replace..."
#     if ! kubectl replace -f ${TMP_FILE}; then
#         echo -e "${RED}Failed to replace PV configuration${NC}"
#         echo "Current PV status:"
#         kubectl get pv ${PV_NAME} -o yaml
#     fi
# fi
# rm ${TMP_FILE}

# # Verify sizes
# echo "Verifying storage size..."
# PV_SIZE=$(kubectl get pv ${PV_NAME} -o jsonpath='{.spec.capacity.storage}')
# echo -e "${YELLOW}Current PV size: ${PV_SIZE}${NC}"

# if [ "${PV_SIZE}" = "100Gi" ]; then
#     echo -e "${GREEN}Storage successfully resized to 100Gi${NC}"
# else
#     echo -e "${RED}Warning: Storage size is ${PV_SIZE}, expected 100Gi${NC}"
#     echo -e "${YELLOW}Continuing with deployment...${NC}"
# fi

# Step 2: Run Charon setup script
echo -e "${YELLOW}Step 2: Running Charon setup script...${NC}"
./run_charon_k8s_v1.0.sh "${ENCLAVE_NAME}"

# Step 3: Copy .env file to project root
echo -e "${YELLOW}Step 3: Copying .env file...${NC}"
cp "${ENCLAVE_NAME}/.env" ./.env

# Step 4: Upload configurations to S3
echo -e "${YELLOW}Step 5: Uploading configurations to S3...${NC}"
cd "${ENCLAVE_NAME}"
aws s3 cp --recursive testnet/ "s3://charon-clusters-config/${NAMESPACE}/testnet/"
aws s3 cp --recursive .charon/cluster/ "s3://charon-clusters-config/${NAMESPACE}/"
cd ..

# Step 5: Create Lighthouse validators definitions if needed
if [ "$VALIDATOR_CLIENT" == "lighthouse" ]; then
    echo -e "${YELLOW}Step 4: Creating Lighthouse validators definitions...${NC}"
    ./create-lighthouse-validators-definitions.sh "${NAMESPACE}"
fi

# Always use the configured AWS credentials
kubectl create secret generic aws-credentials \
    --from-literal=AWS_ACCESS_KEY_ID="${AWS_ACCESS_KEY_ID}" \
    --from-literal=AWS_SECRET_ACCESS_KEY="${AWS_SECRET_ACCESS_KEY}" \
    --from-literal=AWS_SESSION_TOKEN="${AWS_SESSION_TOKEN}" \
    -n "${NAMESPACE}" \
    --dry-run=client -o yaml | kubectl apply -f -

# Step 7: Update genesis timestamp in values file
echo -e "${YELLOW}Step 6: Updating genesis timestamp...${NC}"
if [ -f "${ENCLAVE_NAME}/.env" ]; then
    GENESIS_TIMESTAMP=$(grep TESTNET_GENESIS_TIME_STAMP "${ENCLAVE_NAME}/.env" | cut -d'=' -f2 | tr -d '"')
    if [ -n "$GENESIS_TIMESTAMP" ]; then
        # Create a temporary file
        TMP_FILE=$(mktemp)
        # Replace the timestamp
        awk -v ts="$GENESIS_TIMESTAMP" '{gsub(/TESTNET_GENESIS_TIME_STAMP: "[^"]*"/, "TESTNET_GENESIS_TIME_STAMP: \""ts"\"")}1' "${VALUES_FILE}" > "$TMP_FILE"
        # Move the temporary file back
        mv "$TMP_FILE" "${VALUES_FILE}"
        echo "Updated genesis timestamp to ${GENESIS_TIMESTAMP}"
    else
        echo -e "${RED}Warning: Could not find genesis timestamp in .env file${NC}"
    fi
else
    echo -e "${RED}Warning: .env file not found${NC}"
fi

# Print current values and ask for confirmation
echo -e "\n${YELLOW}Current configuration:${NC}"
echo "Namespace: ${NAMESPACE}"
echo "Values file: ${VALUES_FILE}"
echo "Helm release: ${HELM_RELEASE}"
echo -e "\nPlease verify the following in ${VALUES_FILE}:"
echo "1. TESTNET_GENESIS_TIME_STAMP is correctly set"
echo "2. BEACON_NODE_ENDPOINTS are correct"
echo "3. NUM_VALIDATORS is set to the desired value"
echo -e "\nThe helm command that will be run is:"
echo -e "${GREEN}helm upgrade --install ${HELM_RELEASE} ./kurtosis-charon-vc-helm -f ${VALUES_FILE} -n ${NAMESPACE}${NC}"
echo -e "\nTo modify the deployment later, you can use this command with updated values."

read -p "Do you want to proceed with the deployment? (y/n) " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo -e "${RED}Deployment cancelled by user${NC}"
    exit 1
fi

# Step 8: Deploy Helm chart
echo -e "${YELLOW}Step 8: Deploying Helm chart...${NC}"
helm upgrade --install "${HELM_RELEASE}" ./kurtosis-charon-vc-helm -f "${VALUES_FILE}" -n "${NAMESPACE}"

echo -e "${GREEN}Deployment complete!${NC}"
echo -e "Namespace: ${NAMESPACE}"
echo -e "Helm Release: ${HELM_RELEASE}"
echo -e "Values File: ${VALUES_FILE}"
