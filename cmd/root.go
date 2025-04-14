package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	el      string
	cl      string
	vc      string
	step    int
	skip    string
	version = "0.1.0" // Current version of the application
	verbose bool
)

var rootCmd = &cobra.Command{
	Use:   "kc",
	Short: "Kurtosis Charon CLI for cluster deployment",
	Long: `A CLI tool for deploying Charon clusters using Kurtosis,
Kubernetes, and Helm. This tool orchestrates the entire deployment process
from setting up the execution and consensus clients to deploying validators with charon.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if err := validateInputs(); err != nil {
			return err
		}
		return initializeLogging()
	},
	Run: func(cmd *cobra.Command, args []string) {
		// Main command logic here
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
	rootCmd.PersistentFlags().IntVar(&step, "step", 0, "Run steps up to this number (1-7). If not specified, runs all steps.")
	rootCmd.PersistentFlags().StringVar(&skip, "skip", "0", "Comma-separated list of steps to skip (e.g. 2,3)")
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "Enable verbose logging")

	// Required flags
	rootCmd.MarkPersistentFlagRequired("el")
	rootCmd.MarkPersistentFlagRequired("cl")
	rootCmd.MarkPersistentFlagRequired("vc")
}

// initializeLogging sets up the logging configuration
func initializeLogging() error {
	// Initialize logging with enclave-specific filename
	enclaveName := fmt.Sprintf("kt-%s-%s-%s", el, cl, strings.ReplaceAll(vc, ",", ""))
	logFileName := fmt.Sprintf("kurtosis-charon-%s.log", enclaveName)

	logFile, err := os.OpenFile(logFileName, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return fmt.Errorf("failed to open log file: %v", err)
	}

	// Configure logrus to write to both file and stdout
	logrus.SetOutput(io.MultiWriter(os.Stdout, logFile))

	// Set log format to JSON for better Loki compatibility
	logrus.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: time.RFC3339Nano,
		FieldMap: logrus.FieldMap{
			logrus.FieldKeyTime:  "timestamp",
			logrus.FieldKeyLevel: "level",
			logrus.FieldKeyMsg:   "message",
		},
	})

	// Set log level
	logrus.SetLevel(logrus.InfoLevel)

	// Add common fields for better log correlation
	logrus.AddHook(&LogHook{
		Fields: logrus.Fields{
			"application": "kurtosis-charon",
			"version":     version,
			"enclave":     enclaveName,
		},
	})

	return nil
}

// LogHook adds common fields to all log entries
type LogHook struct {
	Fields logrus.Fields
}

func (hook *LogHook) Fire(entry *logrus.Entry) error {
	for k, v := range hook.Fields {
		entry.Data[k] = v
	}
	return nil
}

func (hook *LogHook) Levels() []logrus.Level {
	return logrus.AllLevels
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
	if step != 0 && (step < 1 || step > 7) {
		return fmt.Errorf("step must be between 1 and 7")
	}

	return nil
}
