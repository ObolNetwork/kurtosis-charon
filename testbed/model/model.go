//go:generate stringer -type ELType -trimprefix ELType
//go:generate stringer -type CLType -trimprefix CLType
//go:generate stringer -type BNType -trimprefix BNType

package model

import (
	"encoding/json"
	"fmt"
	"github.com/obolnetwork/charon/app/errors"
	"strings"
)

type EntityType interface {
	IsEntity() bool
	String() string
}

type ELType int

func (e *ELType) UnmarshalJSON(bytes []byte) error {
	var rawType string
	if err := json.Unmarshal(bytes, &rawType); err != nil {
		return err
	}

	*e = StringToEL(rawType)

	return nil
}

func (e ELType) MarshalJSON() ([]byte, error) {
	ret := strings.ToLower(strings.TrimPrefix(e.String(), "eltype"))
	ret = fmt.Sprintf(`"%s"`, ret)
	return []byte(ret), nil
}

func (e ELType) IsEntity() bool {
	return true
}

type CLType int

func (e CLType) MarshalJSON() ([]byte, error) {
	ret := strings.ToLower(strings.TrimPrefix(e.String(), "cltype"))
	ret = fmt.Sprintf(`"%s"`, ret)
	return []byte(ret), nil
}

func (e *CLType) UnmarshalJSON(bytes []byte) error {
	var rawType string
	if err := json.Unmarshal(bytes, &rawType); err != nil {
		return err
	}

	*e = StringToCL(rawType)

	return nil
}

func (e CLType) IsEntity() bool {
	return true
}

type BNType int

func (e BNType) MarshalJSON() ([]byte, error) {
	ret := strings.ToLower(strings.TrimPrefix(e.String(), "bntype"))
	ret = fmt.Sprintf(`"%s"`, ret)
	return []byte(ret), nil
}

func (e *BNType) UnmarshalJSON(bytes []byte) error {
	var rawType string
	if err := json.Unmarshal(bytes, &rawType); err != nil {
		return err
	}

	*e = StringToBN(rawType)

	return nil
}

func (e BNType) IsEntity() bool {
	return true
}

const (
	ELTypeGeth ELType = iota
	ELTypeUnknown
)

const (
	CLTypeLighthouse CLType = iota
	CLTypeLodestar
	CLTypeNimbus
	CLTypePrysm
	CLTypeTeku
	CLTypeUnknown
)

const (
	BNTypeLighthouse BNType = iota
	BNTypeLodestar
	BNTypeNimbus
	BNTypePrysm
	BNTypeTeku
	BNTypeUnknown
)

var (
	clStrToType = map[string]CLType{
		"lighthouse": CLTypeLighthouse,
		"lodestar":   CLTypeLodestar,
		"nimbus":     CLTypeNimbus,
		"teku":       CLTypeTeku,
		"prysm":      CLTypePrysm,
		"unknown":    CLTypeUnknown,
	}

	bnStrToType = map[string]BNType{
		"lighthouse": BNTypeLighthouse,
		"lodestar":   BNTypeLodestar,
		"nimbus":     BNTypeNimbus,
		"teku":       BNTypeTeku,
		"prysm":      BNTypePrysm,
		"unknown":    BNTypeUnknown,
	}

	elStrToType = map[string]ELType{
		"geth":    ELTypeGeth,
		"unknown": ELTypeUnknown,
	}
)

type NetworkDefinition struct {
	ELType                ELType
	ELDockerImage         string
	CLType                CLType
	CLDockerImage         string
	ValidatorStacksAmount uint64
	ValidatorsPerNode     uint64

	BNType        BNType
	BNDockerImage string
}

func (nd NetworkDefinition) Validate() error {
	if nd.ELType >= ELTypeUnknown {
		return errors.New("unknown EL type")
	}

	if nd.ELDockerImage == "" {
		return errors.New("missing EL docker image")
	}

	if nd.CLType >= CLTypeUnknown {
		return errors.New("unknown CL type")
	}

	if nd.CLDockerImage == "" {
		return errors.New("missing CL docker image")
	}

	if nd.ValidatorStacksAmount == 0 {
		return errors.New("validator stacks amount must not be zero")
	}

	if nd.ValidatorsPerNode == 0 {
		return errors.New("validator stacks amount must not be zero")
	}

	if nd.BNType >= BNTypeUnknown {
		return errors.New("unknown BN type")
	}

	if nd.BNDockerImage == "" {
		return errors.New("missing BN docker image")
	}

	return nil
}

func StringToCL(str string) CLType {
	if cl, ok := clStrToType[str]; !ok {
		return CLTypeUnknown
	} else {
		return cl
	}
}

func StringToBN(str string) BNType {
	if cl, ok := bnStrToType[str]; !ok {
		return BNTypeUnknown
	} else {
		return cl
	}
}

func StringToEL(str string) ELType {
	if cl, ok := elStrToType[str]; !ok {
		return ELTypeUnknown
	} else {
		return cl
	}
}
