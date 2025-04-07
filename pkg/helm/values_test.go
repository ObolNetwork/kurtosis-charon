package helm

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/obol/kurtosis-charon/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestGenerateValuesFile(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "helm-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create test config
	cfg := &config.Config{
		EnclaveName:    "geth-nimbus-0123",
		Namespace:      "kt-geth-nimbus-0123",
		ExecutionLayer: "geth",
		ConsensusLayer: "nimbus",
		ValidatorType:  "0,1,2,3",
		CharonVersion:  "v1.3.0",
		VCVersions: map[string]string{
			"0": "v4.0.0",  // teku
			"1": "v4.0.0",  // lighthouse
			"2": "v1.0.0",  // lodestar
			"3": "v23.1.0", // nimbus
		},
		ValuesFile: filepath.Join(tmpDir, "values.yaml"),
	}

	// Generate values file
	err = GenerateValuesFile(cfg)
	require.NoError(t, err)

	// Read and verify the generated file
	data, err := os.ReadFile(cfg.ValuesFile)
	require.NoError(t, err)

	// Unmarshal the YAML to verify its structure
	var values Values
	err = yaml.Unmarshal(data, &values)
	require.NoError(t, err)

	// Verify the values
	assert.Equal(t, cfg.Namespace, values.CLUSTER_NAME)
	assert.Equal(t, 256, values.NUM_VALIDATORS)
	assert.NotEmpty(t, values.TESTNET_GENESIS_TIME_STAMP)
	assert.Equal(t, 4, len(values.NODES))

	// Verify node configurations
	vcTypes := strings.Split(cfg.ValidatorType, ",")
	for i, node := range values.NODES {
		assert.Equal(t, i, node.Index)
		assert.Equal(t, fmt.Sprintf("node%d", i), node.Name)
		assert.Equal(t, cfg.CharonVersion, node.CHARON_VERSION)
		assert.Equal(t, int(vcTypes[i][0]-'0'), node.VC_TYPE)
		assert.Equal(t, cfg.GetVCVersion(byte(vcTypes[i][0])), node.VC_VERSION)
		assert.Equal(t, cfg.GetBeaconNodeEndpoint(i), node.BEACON_NODE_ENDPOINTS)
	}

	// Verify VC extra args
	assert.Equal(t, "", values.VC_EXTRA_ARGS["lighthouse"])
	assert.Equal(t, "--builder=true --builder.selection=builderalways", values.VC_EXTRA_ARGS["lodestar"])
}

func TestValuesFileContent(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "helm-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create test config
	cfg := &config.Config{
		EnclaveName:    "geth-nimbus-0123",
		Namespace:      "kt-geth-nimbus-0123",
		ExecutionLayer: "geth",
		ConsensusLayer: "nimbus",
		ValidatorType:  "0,1,2,3",
		CharonVersion:  "v1.3.0",
		VCVersions: map[string]string{
			"0": "v4.0.0",  // teku
			"1": "v4.0.0",  // lighthouse
			"2": "v1.0.0",  // lodestar
			"3": "v23.1.0", // nimbus
		},
		ValuesFile: filepath.Join(tmpDir, "values.yaml"),
	}

	// Generate values file
	err = GenerateValuesFile(cfg)
	require.NoError(t, err)

	// Read the generated file
	data, err := os.ReadFile(cfg.ValuesFile)
	require.NoError(t, err)

	// Replace the timestamp with a regex pattern
	content := string(data)
	re := regexp.MustCompile(`TESTNET_GENESIS_TIME_STAMP: "\d+"`)
	content = re.ReplaceAllString(content, `TESTNET_GENESIS_TIME_STAMP: ".*"`)

	// Define the expected content with proper indentation
	expectedContent := `CLUSTER_NAME: kt-geth-nimbus-0123
NUM_VALIDATORS: 256
TESTNET_GENESIS_TIME_STAMP: ".*"
VC_EXTRA_ARGS:
  lighthouse: ""
  lodestar: "--builder=true --builder.selection=builderalways"
NODES:
  - index: 0
    name: node0
    CHARON_VERSION: v1.3.0
    VC_TYPE: 0
    VC_VERSION: v4.0.0
    BEACON_NODE_ENDPOINTS: http://cl-0-nimbus-geth.kt-geth-nimbus-0123.svc.cluster.local:4000
  - index: 1
    name: node1
    CHARON_VERSION: v1.3.0
    VC_TYPE: 1
    VC_VERSION: v4.0.0
    BEACON_NODE_ENDPOINTS: http://cl-1-nimbus-geth.kt-geth-nimbus-0123.svc.cluster.local:4000
  - index: 2
    name: node2
    CHARON_VERSION: v1.3.0
    VC_TYPE: 2
    VC_VERSION: v1.0.0
    BEACON_NODE_ENDPOINTS: http://cl-2-nimbus-geth.kt-geth-nimbus-0123.svc.cluster.local:4000
  - index: 3
    name: node3
    CHARON_VERSION: v1.3.0
    VC_TYPE: 3
    VC_VERSION: v23.1.0
    BEACON_NODE_ENDPOINTS: http://cl-3-nimbus-geth.kt-geth-nimbus-0123.svc.cluster.local:4000
`

	// Compare the content
	assert.Equal(t, expectedContent, content)
}
