package cmd

import (
	"github.com/ObolNetwork/kurtosis-charon/testbed/model"
	"gonum.org/v1/gonum/stat/combin"
)

type EntityConfig[EntityType model.EntityType] struct {
	DockerImage string     `json:"docker_image"`
	Type        EntityType `json:"type"`
}

type enumEC struct {
	ID     int
	Config any
}

type RunConfig struct {
	EL []EntityConfig[model.ELType] `json:"el"`
	BN []EntityConfig[model.BNType] `json:"bn"`
	CL []EntityConfig[model.CLType] `json:"cl"`

	ValidatorStacksAmount uint64 `json:"validator_stacks_amount"`
	ValidatorsPerNode     uint64 `json:"validators_per_node"`
}

func (rc RunConfig) Permutations() []model.NetworkDefinition {
	l := len(rc.BN) + len(rc.CL) + len(rc.EL)

	genMap := func(idx, len int) int {
		return idx % len
	}

	seen := make(map[[3]int]struct{})
	var ret []model.NetworkDefinition

	cs := combin.Combinations(l, 3)
	for _, c := range cs {
		check := [3]int{
			genMap(c[0], len(rc.EL)),
			genMap(c[1], len(rc.CL)),
			genMap(c[2], len(rc.BN)),
		}

		if _, ok := seen[check]; ok {
			continue
		}

		seen[check] = struct{}{}

		el := rc.EL[check[0]]
		cl := rc.CL[check[1]]
		bn := rc.BN[check[2]]

		ret = append(ret, model.NetworkDefinition{
			ELType:                el.Type,
			ELDockerImage:         el.DockerImage,
			CLType:                cl.Type,
			CLDockerImage:         cl.DockerImage,
			ValidatorStacksAmount: rc.ValidatorStacksAmount,
			ValidatorsPerNode:     rc.ValidatorsPerNode,
			BNType:                bn.Type,
			BNDockerImage:         bn.DockerImage,
		})
	}

	return ret
}

func flattenEnumerateBNCL(rc RunConfig) []enumEC {
	var ret []enumEC

	idx := 0

	for _, el := range rc.EL {
		ret = append(ret, enumEC{
			ID:     idx,
			Config: el,
		})

		idx++
	}

	for _, cl := range rc.CL {
		ret = append(ret, enumEC{
			ID:     idx,
			Config: cl,
		})

		idx++
	}

	for _, bn := range rc.BN {
		ret = append(ret, enumEC{
			ID:     idx,
			Config: bn,
		})

		idx++
	}

	return ret
}
