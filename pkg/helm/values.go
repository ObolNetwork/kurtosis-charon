package helm

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/obol/kurtosis-charon/pkg/config"
	"github.com/sirupsen/logrus"
)

type Node struct {
	Index                 int    `yaml:"index"`
	Name                  string `yaml:"name"`
	CHARON_VERSION        string `yaml:"CHARON_VERSION"`
	VC_TYPE               int    `yaml:"VC_TYPE"`
	VC_VERSION            string `yaml:"VC_VERSION"`
	BEACON_NODE_ENDPOINTS string `yaml:"BEACON_NODE_ENDPOINTS"`
}

type Values struct {
	CLUSTER_NAME               string            `yaml:"CLUSTER_NAME"`
	NUM_VALIDATORS             int               `yaml:"NUM_VALIDATORS"`
	TESTNET_GENESIS_TIME_STAMP string            `yaml:"TESTNET_GENESIS_TIME_STAMP"`
	VC_EXTRA_ARGS              map[string]string `yaml:"VC_EXTRA_ARGS"`
	NODES                      []Node            `yaml:"NODES"`
}

func GenerateValuesFile(cfg *config.Config) error {
	// Split validator types by comma
	vcTypes := strings.Split(cfg.ValidatorType, ",")

	// Create nodes based on validator client types
	nodes := make([]Node, 0, len(vcTypes))
	for i, vcType := range vcTypes {
		if vcType == "" {
			continue
		}
		nodes = append(nodes, Node{
			Index:                 i,
			Name:                  fmt.Sprintf("node%d", i),
			CHARON_VERSION:        cfg.CharonVersion,
			VC_TYPE:               int(vcType[0] - '0'),
			VC_VERSION:            cfg.GetVCVersion(byte(vcType[0])),
			BEACON_NODE_ENDPOINTS: cfg.GetBeaconNodeEndpoint(i),
		})
	}

	values := Values{
		CLUSTER_NAME:               cfg.Namespace,
		NUM_VALIDATORS:             256,
		TESTNET_GENESIS_TIME_STAMP: fmt.Sprintf("%d", time.Now().Unix()),
		VC_EXTRA_ARGS: map[string]string{
			"lighthouse": "",
			"lodestar":   "--builder=true --builder.selection=builderalways",
		},
		NODES: nodes,
	}

	// Format the YAML string to match the expected format
	yamlStr := fmt.Sprintf(`CLUSTER_NAME: %s
NUM_VALIDATORS: %d
TESTNET_GENESIS_TIME_STAMP: "%s"
VC_EXTRA_ARGS:
  lighthouse: "%s"
  lodestar: "%s"
NODES:
`,
		values.CLUSTER_NAME,
		values.NUM_VALIDATORS,
		values.TESTNET_GENESIS_TIME_STAMP,
		values.VC_EXTRA_ARGS["lighthouse"],
		values.VC_EXTRA_ARGS["lodestar"],
	)

	// Add nodes with correct indentation
	for _, node := range values.NODES {
		yamlStr += fmt.Sprintf("  - index: %d\n    name: %s\n    CHARON_VERSION: %s\n    VC_TYPE: %d\n    VC_VERSION: %s\n    BEACON_NODE_ENDPOINTS: %s\n",
			node.Index,
			node.Name,
			node.CHARON_VERSION,
			node.VC_TYPE,
			node.VC_VERSION,
			node.BEACON_NODE_ENDPOINTS)
	}

	// Write to file
	err := os.WriteFile(cfg.ValuesFile, []byte(yamlStr), 0644)
	if err != nil {
		return fmt.Errorf("failed to write values file: %v", err)
	}

	logrus.Infof("Generated Helm values file: %s", cfg.ValuesFile)
	return nil
}
