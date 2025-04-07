package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewConfig(t *testing.T) {
	// Set up test environment variables
	os.Setenv("CHARON_VERSION", "v1.3.0")
	os.Setenv("TEKU_VERSION", "v4.0.0")
	os.Setenv("LIGHTHOUSE_VERSION", "v4.0.0")
	os.Setenv("LODESTAR_VERSION", "v1.0.0")
	os.Setenv("NIMBUS_VERSION", "v23.1.0")
	os.Setenv("AWS_REGION", "us-east-1")
	os.Setenv("AWS_BUCKET", "test-bucket")
	os.Setenv("AWS_ACCESS_KEY_ID", "test-key")
	os.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret")

	// Create a temporary network params file
	tmpDir, err := os.MkdirTemp("", "config-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	networkParamsContent := `
services:
  el-1-geth:
    container_name: el-1-geth
    image: ethereum/client-go:v1.13.14
  cl-1-nimbus:
    container_name: cl-1-nimbus
    image: statusim/nimbus-eth2:multiarch-v23.11.0
`
	networkParamsFile := filepath.Join(tmpDir, "network_params_geth_nimbus.yaml")
	err = os.WriteFile(networkParamsFile, []byte(networkParamsContent), 0644)
	require.NoError(t, err)

	// Set the current working directory to the temp directory
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	err = os.Chdir(tmpDir)
	require.NoError(t, err)
	defer os.Chdir(originalWd)

	// Test valid configuration
	cfg, err := NewConfig("geth", "nimbus", "0,1,2,3")
	require.NoError(t, err)
	assert.Equal(t, "geth-nimbus-0123", cfg.EnclaveName)
	assert.Equal(t, "kt-geth-nimbus-0123", cfg.Namespace)
	assert.Equal(t, "v1.3.0", cfg.CharonVersion)
	assert.Equal(t, "v4.0.0", cfg.GetVCVersion('0'))  // teku
	assert.Equal(t, "v4.0.0", cfg.GetVCVersion('1'))  // lighthouse
	assert.Equal(t, "v1.0.0", cfg.GetVCVersion('2'))  // lodestar
	assert.Equal(t, "v23.1.0", cfg.GetVCVersion('3')) // nimbus

	// Test invalid validator type length
	_, err = NewConfig("geth", "nimbus", "0,1,2,3,4")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validator type must only contain characters 0-3")

	// Test invalid validator type characters
	_, err = NewConfig("geth", "nimbus", "0,1,4,3")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validator type must only contain characters 0-3")

	// Test invalid execution layer
	_, err = NewConfig("invalid", "nimbus", "0,1,2,3")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported execution layer")

	// Test invalid consensus layer
	_, err = NewConfig("geth", "invalid", "0,1,2,3")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported consensus layer")
}

func TestGetBeaconNodeEndpoint(t *testing.T) {
	cfg := &Config{
		ExecutionLayer: "geth",
		ConsensusLayer: "nimbus",
		Namespace:      "kt-geth-nimbus-0123",
	}

	// Test with nimbus (port 4000)
	endpoint := cfg.GetBeaconNodeEndpoint(0)
	assert.Equal(t, "http://cl-0-nimbus-geth.kt-geth-nimbus-0123.svc.cluster.local:4000", endpoint)

	// Test with prysm (port 3500)
	cfg.ConsensusLayer = "prysm"
	endpoint = cfg.GetBeaconNodeEndpoint(0)
	assert.Equal(t, "http://cl-0-prysm-geth.kt-geth-nimbus-0123.svc.cluster.local:3500", endpoint)
}
