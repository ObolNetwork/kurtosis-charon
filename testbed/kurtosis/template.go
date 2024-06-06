package kurtosis

import (
	"embed"
	"github.com/ObolNetwork/kurtosis-charon/testbed/model"
	"github.com/obolnetwork/charon/app/errors"
	"io"
	"net/url"
	"strings"
)
import "text/template"

const (
	DefaultValidatorStacksAmount uint64 = 3
	DefaultValidatorsPerNode     uint64 = 600
)

var (
	//go:embed kurtosis_network_params.tmpl
	rawTemplateFS embed.FS
)

// GenerateNetworkParams returns bytes containing a kurtosis network definition, generate given the provided NetworkDefinition.
func GenerateNetworkParams(nd model.NetworkDefinition, writer io.Writer) error {
	funcMap := template.FuncMap{
		"ToLower":     strings.ToLower,
		"URLUnescape": url.QueryUnescape,
	}
	tmpl := template.Must(template.New("kurtosis_network_params.tmpl").Funcs(funcMap).ParseFS(rawTemplateFS, "kurtosis_network_params.tmpl"))

	if err := tmpl.Execute(writer, nd); err != nil {
		return errors.Wrap(err, "cannot generate template with the provided network definition")
	}

	return nil
}
