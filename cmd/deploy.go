package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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

	// Step 1: Cleanup and validation
	if step == 0 || step >= 1 {
		logrus.Info("Step 1: Performing cleanup and validation")
		if err := cleanupAndValidate(cfg); err != nil {
			return fmt.Errorf("cleanup and validation failed: %v", err)
		}
		if step == 1 {
			return nil
		}
	}

	// Step 2: Run Kurtosis plan
	if step == 0 || step >= 2 {
		logrus.Info("Step 2: Running Kurtosis plan")
		if err := runKurtosisPlan(cfg); err != nil {
			return fmt.Errorf("Kurtosis plan failed: %v", err)
		}
		if step == 2 {
			return nil
		}
	}

	// Step 3: Download and generate keys
	if step == 0 || step >= 3 {
		logrus.Info("Step 3: Downloading and generating keys")
		if err := downloadAndGenerateKeys(cfg); err != nil {
			return fmt.Errorf("key generation failed: %v", err)
		}
		if step == 3 {
			return nil
		}
	}

	// Step 4: S3 uploads
	if step == 0 || step >= 4 {
		logrus.Info("Step 4: Uploading to S3")
		if err := uploadToS3(cfg); err != nil {
			return fmt.Errorf("S3 upload failed: %v", err)
		}
		if step == 4 {
			return nil
		}
	}

	// Step 5: Helm deploy
	if step == 0 || step == 5 {
		logrus.Info("Step 5: Deploying with Helm")
		if err := deployWithHelm(cfg); err != nil {
			return fmt.Errorf("Helm deployment failed: %v", err)
		}
	}

	return nil
}

func cleanupAndValidate(cfg *config.Config) error {
	// Clean up old Kubernetes namespaces
	cmd := exec.Command("kubectl", "delete", "namespace", cfg.Namespace)
	if err := cmd.Run(); err != nil {
		logrus.Warnf("Failed to delete namespace %s: %v", cfg.Namespace, err)
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

	// Get enclave UUID
	cmd = exec.Command("kurtosis", "enclave", "ls")
	output, err = cmd.CombinedOutput()
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

	// Get beacon node port using Kurtosis port print
	beaconClient := fmt.Sprintf("cl-1-%s-%s", strings.ToLower(cfg.ConsensusLayer), strings.ToLower(cfg.ExecutionLayer))
	cmd = exec.Command("kurtosis", "port", "print",
		cfg.EnclaveUUID,
		beaconClient,
		"http")

	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to get beacon node port: %v\nOutput: %s", err, string(output))
	}

	// Extract port from output (format: http://127.0.0.1:PORT)
	portOutput := strings.TrimSpace(string(output))
	if !strings.Contains(portOutput, "http://") {
		return fmt.Errorf("unexpected port print output format: %s", portOutput)
	}

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

	// Store genesis timestamp in config
	cfg.GenesisTimestamp = genesis.Data.GenesisTime
	logrus.Infof("Fetched genesis timestamp from beacon node: %s", cfg.GenesisTimestamp)

	// TODO: Create a dedicated function to update specific values in the YAML file
	// instead of regenerating the entire file. This would be more efficient and
	// safer when we only need to update certain fields like genesis timestamp.
	// Current implementation regenerates the entire file which could be problematic
	// if other values have been manually modified.
	if err := helm.GenerateValuesFile(cfg); err != nil {
		return fmt.Errorf("failed to update values file with genesis timestamp: %v", err)
	}
	logrus.Infof("Updated values file with genesis timestamp: %s", cfg.GenesisTimestamp)

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

	// TODO: Implement charon command execution for key generation
	// This will depend on the specific charon command structure

	return nil
}

func uploadToS3(cfg *config.Config) error {
	// TODO: Implement AWS S3 upload logic using cfg.AWS* fields
	return nil
}

func deployWithHelm(cfg *config.Config) error {
	// Create namespace if it doesn't exist
	cmd := exec.Command("kubectl", "create", "namespace", cfg.Namespace)
	if err := cmd.Run(); err != nil {
		logrus.Warnf("Failed to create namespace %s: %v", cfg.Namespace, err)
	}

	// Deploy with Helm
	cmd = exec.Command("helm", "upgrade", "--install",
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
