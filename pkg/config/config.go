package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type Config struct {
	// Cluster configuration
	EnclaveName    string
	Namespace      string
	ExecutionLayer string
	ConsensusLayer string
	ValidatorType  string

	// Version configurations
	CharonVersion string
	VCVersions    map[string]string

	// AWS configuration
	AWSRegion    string
	AWSBucket    string
	AWSAccessKey string
	AWSSecretKey string

	// Network configuration
	NetworkParamsFile string
	ValuesFile        string
}

func NewConfig(executionLayer, consensusLayer, validatorType string) (*Config, error) {
	// Load environment variables
	viper.SetConfigType("env")
	viper.SetConfigName(".env.k8s")
	viper.AddConfigPath(".")
	if err := viper.ReadInConfig(); err != nil {
		logrus.Info("No .env.k8s file found, using environment variables")
	}
	viper.AutomaticEnv()

	// Check if network params file exists
	networkParamsFile := fmt.Sprintf("network_params_%s_%s.yaml", strings.ToLower(executionLayer), strings.ToLower(consensusLayer))
	if _, err := os.Stat(networkParamsFile); err != nil {
		return nil, fmt.Errorf("network params file %s does not exist", networkParamsFile)
	}

	// Create config
	cfg := &Config{
		EnclaveName:    fmt.Sprintf("%s-%s-%s", executionLayer, consensusLayer, strings.ReplaceAll(validatorType, ",", "")),
		Namespace:      fmt.Sprintf("kt-%s-%s-%s", executionLayer, consensusLayer, strings.ReplaceAll(validatorType, ",", "")),
		ExecutionLayer: executionLayer,
		ConsensusLayer: consensusLayer,
		ValidatorType:  validatorType,
		CharonVersion:  viper.GetString("CHARON_VERSION"),
		VCVersions: map[string]string{
			"0": viper.GetString("TEKU_VERSION"),       // teku
			"1": viper.GetString("LIGHTHOUSE_VERSION"), // lighthouse
			"2": viper.GetString("LODESTAR_VERSION"),   // lodestar
			"3": viper.GetString("NIMBUS_VERSION"),     // nimbus
			"4": viper.GetString("PRYSM_VERSION"),      // prysm
		},
		AWSRegion:         viper.GetString("AWS_REGION"),
		AWSBucket:         viper.GetString("AWS_BUCKET"),
		AWSAccessKey:      viper.GetString("AWS_ACCESS_KEY_ID"),
		AWSSecretKey:      viper.GetString("AWS_SECRET_ACCESS_KEY"),
		NetworkParamsFile: networkParamsFile,
		ValuesFile:        fmt.Sprintf("%s-values.yaml", fmt.Sprintf("%s-%s-%s", executionLayer, consensusLayer, strings.ReplaceAll(validatorType, ",", ""))),
	}

	// Validate required configurations
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) validate() error {
	// Validate AWS credentials
	if c.AWSAccessKey == "" || c.AWSSecretKey == "" {
		return fmt.Errorf("AWS credentials not found. Please set AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY")
	}

	// Validate validator client versions
	for _, version := range c.VCVersions {
		if version == "" {
			return fmt.Errorf("validator client version not specified. Please set TEKU_VERSION, LIGHTHOUSE_VERSION, LODESTAR_VERSION, and NIMBUS_VERSION")
		}
	}

	return nil
}

func (c *Config) GetVCVersion(vcType byte) string {
	return c.VCVersions[string(vcType)]
}

func (c *Config) GetBeaconNodeEndpoint(index int) string {
	port := 4000
	if c.ConsensusLayer == "prysm" {
		port = 3500
	}
	return fmt.Sprintf("http://cl-%d-%s-%s.%s.svc.cluster.local:%d",
		index+1, c.ConsensusLayer, c.ExecutionLayer, c.Namespace, port)
}
