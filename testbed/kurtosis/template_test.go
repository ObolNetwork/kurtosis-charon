package kurtosis_test

import (
	"encoding/json"
	"github.com/ObolNetwork/kurtosis-charon/testbed/kurtosis"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestELCLTypes_UnmarshalJSON(t *testing.T) {
	// known type
	nd := kurtosis.NetworkDefinition{
		ELType: kurtosis.ELTypeGeth,
		CLType: kurtosis.CLTypeNimbus,
	}

	data, err := json.Marshal(nd)
	require.NoError(t, err)

	var ndTwo kurtosis.NetworkDefinition
	require.NoError(t, json.Unmarshal(data, &ndTwo))

	require.Equal(t, nd, ndTwo)

	// unknown type
	data = []byte(`{"ELType": "meme", "CLType": "yolo"}`)
	var ndWrong kurtosis.NetworkDefinition
	require.NoError(t, json.Unmarshal(data, &ndWrong))
	require.Equal(t, kurtosis.ELTypeUnknown, ndWrong.ELType)
	require.Equal(t, kurtosis.CLTypeUnknown, ndWrong.CLType)
}
