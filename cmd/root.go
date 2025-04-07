package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	el      string
	cl      string
	vc      string
	step    int
	verbose bool
)

var rootCmd = &cobra.Command{
	Use:   "kc",
	Short: "Kurtosis Charon CLI for cluster deployment",
	Long: `A CLI tool for deploying Charon clusters using Kurtosis,
Kubernetes, and Helm. This tool orchestrates the entire deployment process
from setting up the execution and consensus clients to deploying validators with charon.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := validateInputs(); err != nil {
			logrus.Fatal(err)
		}
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&el, "el", "e", "", "Execution layer client (e.g. geth, nethermind)")
	rootCmd.PersistentFlags().StringVarP(&cl, "cl", "c", "", "Consensus layer client (e.g. nimbus, lighthouse)")
	rootCmd.PersistentFlags().StringVarP(&vc, "vc", "v", "", "Validator client type encoding (e.g. 0,0,1,2 for two Teku, one Lighthouse, and one Lodestar)")
	rootCmd.PersistentFlags().IntVar(&step, "step", 0, "Run steps up to this number (1-5). If not specified, runs all steps.")
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "Enable verbose logging")

	// Required flags
	rootCmd.MarkPersistentFlagRequired("el")
	rootCmd.MarkPersistentFlagRequired("cl")
	rootCmd.MarkPersistentFlagRequired("vc")
}

func validateInputs() error {
	// Validate execution layer
	validELs := map[string]bool{
		"geth":       true,
		"nethermind": true,
	}
	if !validELs[el] {
		return fmt.Errorf("invalid execution layer: %s", el)
	}

	// Validate consensus layer
	validCLs := map[string]bool{
		"nimbus":     true,
		"lighthouse": true,
		"lodestar":   true,
		"prysm":      true,
		"teku":       true,
	}
	if !validCLs[cl] {
		return fmt.Errorf("invalid consensus layer: %s", cl)
	}

	// Validate validator client encoding
	validVCs := map[string]string{
		"0": "teku",
		"1": "lighthouse",
		"2": "lodestar",
		"3": "nimbus",
		"4": "prysm",
	}
	vcTypes := strings.Split(vc, ",")
	for _, vcType := range vcTypes {
		if vcType == "" {
			continue
		}
		if _, valid := validVCs[vcType]; !valid {
			return fmt.Errorf("invalid validator type: %s. Valid types are: 0 (teku), 1 (lighthouse), 2 (lodestar), 3 (nimbus), 4 (prysm)", vcType)
		}
	}

	// Validate step if provided
	if step != 0 && (step < 1 || step > 5) {
		return fmt.Errorf("step must be between 1 and 5")
	}

	return nil
}
