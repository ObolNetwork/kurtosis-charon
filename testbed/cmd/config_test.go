package cmd_test

import (
	"encoding/json"
	"github.com/ObolNetwork/kurtosis-charon/testbed/cmd"
	"github.com/ObolNetwork/kurtosis-charon/testbed/model"
	"github.com/stretchr/testify/require"
	"testing"
)

func Test_MarshalUnmarshal(t *testing.T) {
	rc := cmd.RunConfig{
		EL: []cmd.EntityConfig[model.ELType]{
			{
				DockerImage: "test/testEL:latest",
				Type:        model.ELTypeGeth,
			},
		},
		BN: []cmd.EntityConfig[model.BNType]{
			{
				DockerImage: "test/testBN:latest",
				Type:        model.BNTypePrysm,
			},
		},
		CL: []cmd.EntityConfig[model.CLType]{
			{
				DockerImage: "test/testCL:latest",
				Type:        model.CLTypeNimbus,
			},
		},
		ValidatorStacksAmount: 3,
		ValidatorsPerNode:     600,
	}

	data, err := json.Marshal(rc)
	require.NoError(t, err)

	var rcTwo cmd.RunConfig
	require.NoError(t, json.Unmarshal(data, &rcTwo))

	require.Equal(t, rc, rcTwo)
}

func Test_Permutations(t *testing.T) {
	rc := cmd.RunConfig{
		EL: []cmd.EntityConfig[model.ELType]{
			{
				DockerImage: "test/testEL:latest",
				Type:        model.ELTypeGeth,
			},
		},
		BN: []cmd.EntityConfig[model.BNType]{
			{
				DockerImage: "test/testBNNimbus:latest",
				Type:        model.BNTypePrysm,
			},
			{
				DockerImage: "test/testBNLodestar:latest",
				Type:        model.BNTypeLodestar,
			},
		},
		CL: []cmd.EntityConfig[model.CLType]{
			{
				DockerImage: "test/testCLTeku:latest",
				Type:        model.CLTypeTeku,
			},
			{
				DockerImage: "test/testCLLighthouse:latest",
				Type:        model.CLTypeLighthouse,
			},
		},
		ValidatorStacksAmount: 3,
		ValidatorsPerNode:     600,
	}

	j, err := json.Marshal(rc)
	require.NoError(t, err)

	t.Log(string(j))

	p := rc.Permutations()

	for _, pp := range p {
		t.Logf("%+v\n", pp)
	}
}
