package server

import (
	_ "embed"
	"encoding/json"

	"gopkg.in/yaml.v3"
)

//go:embed openapi.yaml
var openapiSpecYAML []byte

// openapiJSON holds the OpenAPI spec as JSON, converted once at init time.
var openapiJSON []byte

func init() {
	var doc any
	if err := yaml.Unmarshal(openapiSpecYAML, &doc); err != nil {
		panic("openapi.yaml is invalid YAML: " + err.Error())
	}
	b, err := json.Marshal(doc)
	if err != nil {
		panic("openapi.yaml cannot be marshalled to JSON: " + err.Error())
	}
	openapiJSON = b
}
