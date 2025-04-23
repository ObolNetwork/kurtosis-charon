package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/obol/kurtosis-charon/pkg/config"
	"github.com/obol/kurtosis-charon/pkg/helm"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy an Ethereum validator cluster",
	Long: `Deploy an Ethereum validator cluster using the specified execution layer,
consensus layer, and validator client configuration.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := validateInputs(); err != nil {
			logrus.Fatal(err)
		}
		if err := deployCluster(); err != nil {
			logrus.Fatal(err)
		}
	},
}

func init() {
	rootCmd.AddCommand(deployCmd)
}

func deployCluster() error {
	// Create config
	cfg, err := config.NewConfig(el, cl, vc)
	if err != nil {
		return fmt.Errorf("failed to create config: %v", err)
	}

	// Parse skipped steps
	skippedSteps := make(map[int]bool)
	if skip != "" {
		for _, s := range strings.Split(skip, ",") {
			stepNum, err := strconv.Atoi(strings.TrimSpace(s))
			if err != nil {
				return fmt.Errorf("invalid skip step number: %v", err)
			}
			if stepNum < 1 || stepNum > 7 {
				return fmt.Errorf("skip step number must be between 1 and 7")
			}
			skippedSteps[stepNum] = true
		}
	}

	// Step 1: Cleanup and validation
	if step == 0 || step >= 1 && !skippedSteps[1] {
		logrus.Info("Step 1: Performing cleanup and validation")
		if err := cleanupAndValidate(cfg); err != nil {
			return fmt.Errorf("cleanup and validation failed: %v", err)
		}
		if step == 1 {
			return nil
		}
	} else if skippedSteps[1] {
		logrus.Info("Skipping step 1: Cleanup and validation")
	}

	// Step 2: Run Kurtosis plan
	if (step == 0 || step >= 2) && !skippedSteps[2] {
		logrus.Info("Step 2: Running Kurtosis plan")
		if err := runKurtosisPlan(cfg); err != nil {
			return fmt.Errorf("Kurtosis plan failed: %v", err)
		}
		if step == 2 {
			return nil
		}
	} else if skippedSteps[2] {
		logrus.Info("Skipping step 2: Running Kurtosis plan")
		// Even when skipping step 2, we need to fetch the enclave details
		if err := fetchEnclaveDetails(cfg, FetchEnclaveOptions{
			SkipTestnetDownload: true,
		}); err != nil {
			return fmt.Errorf("failed to fetch enclave details: %v", err)
		}
	}

	// Step 3: Download and generate keys
	if (step == 0 || step >= 3) && !skippedSteps[3] {
		logrus.Info("Step 3: Downloading and generating keys")
		if err := downloadAndGenerateKeys(cfg); err != nil {
			return fmt.Errorf("key generation failed: %v", err)
		}
		if step == 3 {
			return nil
		}
	} else if skippedSteps[3] {
		logrus.Info("Skipping step 3: Downloading and generating keys")
	}

	// Step 4: Run Charon cluster creation
	if (step == 0 || step >= 4) && !skippedSteps[4] {
		logrus.Info("Step 4: Running Charon cluster creation")
		if err := runCharonCluster(cfg); err != nil {
			return fmt.Errorf("Charon cluster creation failed: %v", err)
		}
		if step == 4 {
			return nil
		}
	} else if skippedSteps[4] {
		logrus.Info("Skipping step 4: Running Charon cluster creation")
	}

	// Step 5: S3 uploads
	if (step == 0 || step >= 5) && !skippedSteps[5] {
		logrus.Info("Step 5: Uploading to S3")
		if err := uploadToS3(cfg); err != nil {
			return fmt.Errorf("S3 upload failed: %v", err)
		}
		if step == 5 {
			return nil
		}
	} else if skippedSteps[5] {
		logrus.Info("Skipping step 5: Uploading to S3")
	}

	// Step 6: Create AWS secret
	if (step == 0 || step >= 6) && !skippedSteps[6] {
		logrus.Info("Step 6: Creating AWS secret")
		if err := createAWSSecret(cfg); err != nil {
			return fmt.Errorf("AWS secret creation failed: %v", err)
		}
		if step == 6 {
			return nil
		}
	} else if skippedSteps[6] {
		logrus.Info("Skipping step 6: Creating AWS secret")
	}

	// Step 7: Helm deploy
	if (step == 0 || step >= 7) && !skippedSteps[7] {
		logrus.Info("Step 7: Deploying with Helm")
		if err := deployWithHelm(cfg); err != nil {
			return fmt.Errorf("Helm deployment failed: %v", err)
		}

		// Delete the Kurtosis VC pod after Helm deployment
		podName := fmt.Sprintf("vc-1-%s-%s", strings.ToLower(cfg.ExecutionLayer), strings.ToLower(cfg.ConsensusLayer))
		cmd := exec.Command("kubectl", "delete", "pod", podName, "-n", fmt.Sprintf("kt-%s", cfg.EnclaveName))
		output, err := cmd.CombinedOutput()
		if err != nil {
			logrus.Warnf("Failed to delete pod %s: %v\nOutput: %s", podName, err, string(output))
		} else {
			logrus.Infof("Successfully deleted pod %s", podName)
		}

		if step == 7 {
			return nil
		}
	} else if skippedSteps[7] {
		logrus.Info("Skipping step 7: Deploying with Helm")
	}

	return nil
}

func cleanupAndValidate(cfg *config.Config) error {
	// Clean up old Kubernetes namespaces
	cmd := exec.Command("kubectl", "delete", "namespace", cfg.Namespace)
	if err := cmd.Run(); err != nil {
		logrus.Warnf("Failed to delete namespace %s: %v", cfg.Namespace, err)
	}

	// Clean up Kurtosis cluster roles
	cmd = exec.Command("kubectl", "delete", "$(kubectl", "get", "clusterrole", "-o", "name", "|", "grep", "'kurtosis')")
	output, err := cmd.CombinedOutput()
	if err != nil {
		logrus.Warnf("Failed to delete Kurtosis cluster roles: %v\nOutput: %s", err, string(output))
	} else {
		logrus.Info("Successfully deleted Kurtosis cluster roles")
	}

	// Clean up Kurtosis cluster role bindings
	cmd = exec.Command("kubectl", "delete", "$(kubectl", "get", "clusterrolebindings", "-o", "name", "|", "grep", "'kurtosis')")
	output, err = cmd.CombinedOutput()
	if err != nil {
		logrus.Warnf("Failed to delete Kurtosis cluster role bindings: %v\nOutput: %s", err, string(output))
	} else {
		logrus.Info("Successfully deleted Kurtosis cluster role bindings")
	}

	// Clean up local folders and files
	dirs := []string{
		cfg.TestnetDir,
		cfg.KeystoreDir,
		cfg.CharonDir,
		cfg.ClusterDir,
		".charon",
		"keystore",
		"cluster",
	}
	for _, dir := range dirs {
		if err := os.RemoveAll(dir); err != nil {
			logrus.Warnf("Failed to remove directory %s: %v", dir, err)
		}
	}

	// Clean up existing files
	files := []string{
		cfg.ValuesFile,
		cfg.PlanOutputFile,
		fmt.Sprintf("planprint-%s", cfg.EnclaveName),
	}
	for _, file := range files {
		if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
			logrus.Warnf("Failed to remove file %s: %v", file, err)
		}
	}

	// Generate Helm values file
	if err := helm.GenerateValuesFile(cfg); err != nil {
		return fmt.Errorf("failed to generate Helm values file: %v", err)
	}

	return nil
}

func runKurtosisPlan(cfg *config.Config) error {
	// Run Kurtosis plan
	cmd := exec.Command("kurtosis", "run",
		"--enclave", cfg.EnclaveName,
		"github.com/ethpandaops/ethereum-package",
		"--args-file", cfg.NetworkParamsFile)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run Kurtosis plan: %v\nOutput: %s", err, string(output))
	}

	// Store the output in a file
	if err := os.WriteFile(cfg.PlanOutputFile, output, 0644); err != nil {
		return fmt.Errorf("failed to write plan output: %v", err)
	}

	// Fetch enclave details with testnet download
	if err := fetchEnclaveDetails(cfg, FetchEnclaveOptions{
		SkipTestnetDownload: false,
	}); err != nil {
		return fmt.Errorf("failed to fetch enclave details: %v", err)
	}

	logrus.Info("Kurtosis plan completed successfully")
	return nil
}

func downloadAndGenerateKeys(cfg *config.Config) error {
	// Create keystore directory
	if err := os.MkdirAll(cfg.KeystoreDir, 0755); err != nil {
		return fmt.Errorf("failed to create keystore directory: %v", err)
	}

	// Download validator keystores
	cmd := exec.Command("kurtosis", "files", "download",
		cfg.EnclaveUUID,
		fmt.Sprintf("1-%s-%s-0-255", strings.ToLower(cfg.ConsensusLayer), cfg.ExecutionLayer),
		cfg.KeystoreDir)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to download keystores: %v\nOutput: %s", err, string(output))
	}

	// Create charon directory
	if err := os.MkdirAll(cfg.CharonDir, 0755); err != nil {
		return fmt.Errorf("failed to create charon directory: %v", err)
	}

	// Process keystores
	index := 0
	keysDir := filepath.Join(cfg.KeystoreDir, "keys")
	entries, err := os.ReadDir(keysDir)

	logrus.Infof("Processing keystore directory: %s", keysDir)
	if err != nil {
		return fmt.Errorf("failed to read keystore directory: %v", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pubkeysDir := filepath.Join(keysDir, entry.Name())
		logrus.Infof("Processing keystore directory: %s", pubkeysDir)

		// Copy voting-keystore.json to charon-keys with indexed name
		srcKeystore := filepath.Join(pubkeysDir, "voting-keystore.json")
		dstKeystore := filepath.Join(cfg.CharonDir, fmt.Sprintf("keystore-%d.json", index))
		if err := copyFile(srcKeystore, dstKeystore); err != nil {
			return fmt.Errorf("failed to copy keystore file: %v", err)
		}
		logrus.Infof("Copied 'voting-keystore.json' to 'charon-keys' as 'keystore-%d.json'", index)

		// Check for matching secret file
		secretFile := filepath.Join(cfg.KeystoreDir, "secrets", entry.Name())
		if _, err := os.Stat(secretFile); err == nil {
			dstSecret := filepath.Join(cfg.CharonDir, fmt.Sprintf("keystore-%d.txt", index))
			if err := copyFile(secretFile, dstSecret); err != nil {
				return fmt.Errorf("failed to copy secret file: %v", err)
			}
			logrus.Infof("Copied '%s' from 'keystore-secrets' to 'charon-keys' as 'keystore-%d.txt'", entry.Name(), index)
		} else {
			logrus.Warnf("No matching file found in 'keystore-secrets' for '%s'", entry.Name())
		}

		index++
	}

	return nil
}

func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

func uploadToS3(cfg *config.Config) error {
	// Delete existing S3 folder
	s3Path := fmt.Sprintf("s3://%s/%s", cfg.AWSBucket, cfg.Namespace)
	cmd := exec.Command("aws", "s3", "rm", "--recursive", s3Path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		logrus.Warnf("Failed to delete existing S3 folder %s: %v\nOutput: %s", s3Path, err, string(output))
	} else {
		logrus.Info("Successfully deleted existing S3 folder")
	}

	// Upload testnet files to S3 using AWS CLI
	cmd = exec.Command("aws", "s3", "cp", "--recursive", cfg.TestnetDir,
		fmt.Sprintf("s3://%s/%s/testnet", cfg.AWSBucket, cfg.Namespace))

	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to upload to S3: %v\nOutput: %s", err, string(output))
	}
	logrus.Info("Successfully uploaded testnet files to S3")

	// Upload Charon cluster files to S3 using AWS CLI
	cmd = exec.Command("aws", "s3", "cp", "--recursive", cfg.ClusterDir,
		fmt.Sprintf("s3://%s/%s", cfg.AWSBucket, cfg.Namespace))

	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to upload to S3: %v\nOutput: %s", err, string(output))
	}
	logrus.Info("Successfully uploaded Charon cluster files to S3")

	return nil
}

func createAWSSecret(cfg *config.Config) error {
	// Create or update the AWS credentials secret
	cmd := exec.Command("kubectl", "create", "secret", "generic", "aws-credentials",
		"--namespace", cfg.Namespace,
		"--from-literal=AWS_ACCESS_KEY_ID="+cfg.AWSAccessKey,
		"--from-literal=AWS_SECRET_ACCESS_KEY="+cfg.AWSSecretKey,
		"--from-literal=AWS_SESSION_TOKEN="+cfg.AWSSessionToken,
		"--dry-run=client",
		"-o", "yaml")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create AWS credentials secret: %v\nOutput: %s", err, string(output))
	}

	// Apply the secret
	cmd = exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(string(output))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to apply AWS credentials secret: %v", err)
	}

	logrus.Info("Successfully created AWS credentials secret")
	return nil
}

func deployWithHelm(cfg *config.Config) error {
	// Create Lighthouse validator definitions if VC contains Lighthouse
	if cfg.HasValidatorClient("1") {
		logrus.Info("Creating Lighthouse validator definitions...")
		cmd := exec.Command("./create-lighthouse-validators-definitions.sh", cfg.Namespace)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to create Lighthouse validator definitions: %v", err)
		}
	}

	// Deploy with Helm
	cmd := exec.Command("helm", "upgrade", "--install",
		cfg.EnclaveName,
		"kurtosis-charon-vc-helm",
		"--namespace", cfg.Namespace,
		"--values", cfg.ValuesFile)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to deploy with Helm: %v\nOutput: %s", err, string(output))
	}

	logrus.Info("Helm deployment completed successfully")
	return nil
}

func runCharonCluster(cfg *config.Config) error {
	// Create cluster directory if it doesn't exist
	if err := os.MkdirAll(cfg.ClusterDir, 0755); err != nil {
		return fmt.Errorf("failed to create cluster directory: %v", err)
	}

	// Get current user's UID and GID
	uid := os.Getuid()
	gid := os.Getgid()

	// Get current working directory
	pwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %v", err)
	}

	// Build command arguments
	args := []string{
		"run",
		"-u", fmt.Sprintf("%d:%d", uid, gid),
		"--rm",
		"-v", fmt.Sprintf("%s:/opt/charon", pwd),
		fmt.Sprintf("obolnetwork/charon:%s", cfg.CharonVersion),
		"create", "cluster",
		"--fee-recipient-addresses=0x8943545177806ED17B9F23F0a21ee5948eCaa776",
		fmt.Sprintf("--nodes=%d", cfg.NumNodes),
		"--withdrawal-addresses=0xBc7c960C1097ef1Af0FD32407701465f3c03e407",
		fmt.Sprintf("--name=%s", cfg.EnclaveName),
		"--split-existing-keys",
		fmt.Sprintf("--split-keys-dir=%s", cfg.CharonDir),
		"--testnet-chain-id=3151908",
		"--testnet-fork-version=0x10000038",
		fmt.Sprintf("--testnet-genesis-timestamp=%s", cfg.GenesisTimestamp),
		"--testnet-name=kurtosis-testnet",
	}

	// Log the command that will be executed
	logrus.Infof("Executing Charon command: docker %s", strings.Join(args, " "))

	// Run Charon cluster creation
	cmd := exec.Command("docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create Charon cluster: %v\nOutput: %s", err, string(output))
	}

	// Move node folders to cluster directory
	entries, err := os.ReadDir(pwd)
	if err != nil {
		return fmt.Errorf("failed to read current directory: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "node") {
			srcPath := filepath.Join(pwd, entry.Name())
			dstPath := filepath.Join(cfg.ClusterDir, entry.Name())

			// Remove destination if it exists
			if err := os.RemoveAll(dstPath); err != nil {
				return fmt.Errorf("failed to remove existing node directory %s: %v", dstPath, err)
			}

			// Move the directory
			if err := os.Rename(srcPath, dstPath); err != nil {
				return fmt.Errorf("failed to move node directory %s to %s: %v", srcPath, dstPath, err)
			}
			logrus.Infof("Moved %s to %s", srcPath, dstPath)
		}
	}

	// Create .env.k8s file
	envContent := fmt.Sprintf(`NETWORK_NAME=%s
CL_NAME=%s
CLUSTER_NAME=kt-%s
NODES=%d
NUM_VALIDATORS=%d
CHARON_VERSIONS=%s
TEKU_VERSION=%s
LIGHTHOUSE_VERSION=%s
LODESTAR_VERSION=%s
PRYSM_VERSION=%s
NIMBUS_VERSION=%s
VC_TYPES=%s
BEACON_NODE_ADDRESS=host.docker.internal:5052
PROPOSER_DEFAULT_FEE_RECIPIENT=0x50Af11554713D43794b2ACDb351EEB363b03f97e
`,
		cfg.EnclaveName,
		cfg.ConsensusLayer,
		cfg.EnclaveName,
		cfg.NumNodes,
		cfg.NumValidators,
		cfg.CharonVersion,
		cfg.VCVersions["0"], // teku
		cfg.VCVersions["1"], // lighthouse
		cfg.VCVersions["2"], // lodestar
		cfg.VCVersions["4"], // prysm
		cfg.VCVersions["3"], // nimbus
		cfg.VCTypes,
	)

	envPath := filepath.Join(cfg.ClusterDir, ".env")
	if err := os.WriteFile(envPath, []byte(envContent), 0644); err != nil {
		return fmt.Errorf("failed to create .env.k8s file: %v", err)
	}
	logrus.Info("Created .env.k8s file")

	// Delete prometheus service
	cmd = exec.Command("kubectl", "delete", "service", "prometheus", "-n", fmt.Sprintf("kt-%s", cfg.EnclaveName))
	if err := cmd.Run(); err != nil {
		logrus.Warnf("Failed to delete prometheus service: %v", err)
	} else {
		logrus.Info("Deleted prometheus service")
	}

	logrus.Info("Charon cluster created successfully")
	return nil
}

// FetchEnclaveOptions defines options for fetching enclave details
type FetchEnclaveOptions struct {
	SkipTestnetDownload bool
	// Add more options here as needed
}

// fetchEnclaveDetails fetches necessary details from the enclave
func fetchEnclaveDetails(cfg *config.Config, opts ...FetchEnclaveOptions) error {
	// Default options
	options := FetchEnclaveOptions{}
	if len(opts) > 0 {
		options = opts[0]
	}

	// Get enclave UUID
	cmd := exec.Command("kurtosis", "enclave", "ls")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to list enclaves: %v", err)
	}

	// Parse the output to find our enclave
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, cfg.EnclaveName) {
			fields := strings.Fields(line)
			if len(fields) > 0 {
				cfg.EnclaveUUID = fields[0]
				break
			}
		}
	}

	if cfg.EnclaveUUID == "" {
		return fmt.Errorf("failed to find enclave UUID for %s", cfg.EnclaveName)
	}

	// Download testnet files if not skipped
	if !options.SkipTestnetDownload {
		// Create testnet directory
		if err := os.MkdirAll(cfg.TestnetDir, 0755); err != nil {
			return fmt.Errorf("failed to create testnet directory: %v", err)
		}

		// Download testnet files
		cmd = exec.Command("kurtosis", "files", "download",
			cfg.EnclaveUUID,
			"el_cl_genesis_data",
			cfg.TestnetDir)

		output, err = cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to download testnet files: %v\nOutput: %s", err, string(output))
		}
		logrus.Info("Successfully downloaded testnet files")
	} else {
		logrus.Info("Skipping testnet files download as requested")
	}

	// Get beacon node port using Kurtosis port print
	beaconClient := fmt.Sprintf("cl-1-%s-%s", strings.ToLower(cfg.ConsensusLayer), strings.ToLower(cfg.ExecutionLayer))
	logrus.Infof("Beacon client for getting genesis timestamp: %s", beaconClient)
	cmd = exec.Command("kurtosis", "port", "print",
		cfg.EnclaveUUID,
		beaconClient,
		"http")

	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to get beacon node port: %v\nOutput: %s", err, string(output))
	}

	// Extract port from output using regex to match http://IP:PORT pattern
	re := regexp.MustCompile(`http://[0-9\.]+:[0-9]+`)
	portOutput := re.FindString(string(output))
	if portOutput == "" {
		return fmt.Errorf("failed to extract port from output: %s", string(output))
	}
	logrus.Infof("Beacon node port: %s", portOutput)

	// Get genesis timestamp from beacon node using local port
	cmd = exec.Command("curl", "-s",
		fmt.Sprintf("%s/eth/v1/beacon/genesis", portOutput))

	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to get genesis timestamp: %v\nOutput: %s", err, string(output))
	}

	// Parse genesis timestamp from JSON response
	type GenesisResponse struct {
		Data struct {
			GenesisTime string `json:"genesis_time"`
		} `json:"data"`
	}

	var genesis GenesisResponse
	if err := json.Unmarshal(output, &genesis); err != nil {
		return fmt.Errorf("failed to parse genesis response: %v", err)
	}

	if genesis.Data.GenesisTime == "" {
		return fmt.Errorf("genesis timestamp not found in response: %s", string(output))
	}

	cfg.GenesisTimestamp = genesis.Data.GenesisTime

	// TODO: Create a dedicated function to update specific values in the YAML file
	// instead of regenerating the entire file. This would be more efficient and
	// safer when we only need to update certain fields like genesis timestamp.
	// Current implementation regenerates the entire file which could be problematic
	// if other values have been manually modified.
	if err := helm.GenerateValuesFile(cfg); err != nil {
		return fmt.Errorf("failed to update values file with genesis timestamp: %v", err)
	}
	logrus.Infof("Updated values file with genesis timestamp: %s", cfg.GenesisTimestamp)
	return nil
}
