package kurtosis

import (
	"bytes"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestGenerateNetworkParams(t *testing.T) {
	nd := NetworkDefinition{
		ELType:                ELTypeGeth,
		ELDockerImage:         "ethereum/client-go:latest",
		CLType:                CLTypeLighthouse,
		CLDockerImage:         "sigp/lighthouse:v5.1.3",
		ValidatorStacksAmount: 600,
		ValidatorsPerNode:     3,
	}

	var ndBytes bytes.Buffer
	err := GenerateNetworkParams(nd, &ndBytes)
	require.NoError(t, err)

	require.NotEmpty(t, ndBytes.Bytes())

	t.Log(string(ndBytes.Bytes()))
}
