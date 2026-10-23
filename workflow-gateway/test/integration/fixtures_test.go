//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestWorkflowFixtures_HelloYAML(t *testing.T) {
	helloPath := filepath.Join("testdata", "workflows", "hello.yaml")
	data, err := os.ReadFile(helloPath)
	require.NoError(t, err, "hello.yaml must be readable")

	var doc map[string]interface{}
	err = yaml.Unmarshal(data, &doc)
	require.NoError(t, err, "hello.yaml must parse as YAML")

	docNode := doc["document"].(map[string]interface{})
	assert.Equal(t, "demo", docNode["namespace"], "namespace must be demo")
	assert.Equal(t, "hello-world", docNode["name"], "name must be hello-world")
	assert.Equal(t, "1.0.0", docNode["version"], "version must be 1.0.0")
}

func TestWorkflowFixtures_SleepyYAML(t *testing.T) {
	sleepyPath := filepath.Join("testdata", "workflows", "sleepy.yaml")
	data, err := os.ReadFile(sleepyPath)
	require.NoError(t, err, "sleepy.yaml must be readable")

	var doc map[string]interface{}
	err = yaml.Unmarshal(data, &doc)
	require.NoError(t, err, "sleepy.yaml must parse as YAML")

	docNode := doc["document"].(map[string]interface{})
	assert.Equal(t, "demo", docNode["namespace"], "namespace must be demo")
	assert.Equal(t, "sleepy", docNode["name"], "name must be sleepy")
	assert.Equal(t, "1.0.0", docNode["version"], "version must be 1.0.0")
}
