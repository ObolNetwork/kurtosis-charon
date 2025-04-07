package cmd

import (
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

	// Clean up local folders
	dirs := []string{
		".charon",
		"keystore",
		"cluster",
	}
	for _, dir := range dirs {
		if err := os.RemoveAll(dir); err != nil {
			logrus.Warnf("Failed to remove directory %s: %v", dir, err)
		}
	}

	// Generate Helm values file
	if err := helm.GenerateValuesFile(cfg); err != nil {
		return fmt.Errorf("failed to generate Helm values file: %v", err)
	}

	return nil
}

func runKurtosisPlan(cfg *config.Config) error {
	cmd := exec.Command("kurtosis", "run",
		"--enclave", cfg.EnclaveName,
		"github.com/ethpandaops/ethereum-package",
		"--args-file", cfg.NetworkParamsFile)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run Kurtosis plan: %v\nOutput: %s", err, string(output))
	}

	logrus.Info("Kurtosis plan completed successfully")
	return nil
}

func downloadAndGenerateKeys(cfg *config.Config) error {
	keystoreDir := "keystore"

	// Download validator keystores
	cmd := exec.Command("kurtosis", "files", "download",
		cfg.EnclaveName,
		fmt.Sprintf("1-%s-%s-0-255", strings.ToLower(cfg.ConsensusLayer), cfg.ExecutionLayer),
		keystoreDir)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to download keystores: %v\nOutput: %s", err, string(output))
	}

	// Generate charon-keys structure
	if err := os.MkdirAll(".charon", 0755); err != nil {
		return fmt.Errorf("failed to create .charon directory: %v", err)
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
