package configuration

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoadsFileExists(t *testing.T) {
	// Create a temporary file
	tempFile, _ := ioutil.TempFile("", "test")
	defer os.Remove(tempFile.Name())

	// Write some YAML to the file
	yamlData := `
sources: 
  - ./myfolder1
exclude: []

reports:
  html: ./build/report
  markdown: ./build/report.md

requirements:
  rules:
    # with comment
    cyclomatic_complexity:
      max: 10
      excludes: []

    lines_of_code:
      max: 100
      excludes: []

`
	os.WriteFile(tempFile.Name(), []byte(yamlData), 0644)

	// Create a ConfigurationLoader with the temp file name
	loader := &ConfigurationLoader{FilenameToChecks: []string{tempFile.Name()}}

	// Call Loads
	cfg, err := loader.Loads(&Configuration{})

	// Assert no error
	assert.NoError(t, err)

	// Assert the configuration was loaded correctly
	assert.Equal(t, []string{"./myfolder1"}, cfg.SourcesToAnalyzePath)
}

func TestLoadsFileDoesNotExist(t *testing.T) {
	// Create a ConfigurationLoader with a non-existent file name
	loader := &ConfigurationLoader{FilenameToChecks: []string{"non_existent_file.yaml"}}

	// Call Loads
	cfg, err := loader.Loads(&Configuration{})

	// Assert no error
	assert.NoError(t, err)

	// Assert the configuration is empty
	assert.Equal(t, &Configuration{}, cfg)
}

func TestLoadsErrorDecodingYAML(t *testing.T) {
	// Create a temporary file
	tempFile, _ := ioutil.TempFile("", "test")
	defer os.Remove(tempFile.Name())

	// Write some invalid YAML to the file
	invalidYAML := `key: value:`
	ioutil.WriteFile(tempFile.Name(), []byte(invalidYAML), 0644)

	// Create a ConfigurationLoader with the temp file name
	loader := &ConfigurationLoader{FilenameToChecks: []string{tempFile.Name()}}

	// Call Loads
	cfg, err := loader.Loads(&Configuration{})

	// Assert there was an error
	assert.Error(t, err)

	// Assert the configuration is empty
	assert.Equal(t, &Configuration{}, cfg)
}

func TestLoadsExtensions(t *testing.T) {
	tempFile, _ := ioutil.TempFile("", "test")
	defer os.Remove(tempFile.Name())

	yamlData := `
sources:
  - ./
extensions:
  php: [".inc", ".module"]
  go: [".go2"]
`
	os.WriteFile(tempFile.Name(), []byte(yamlData), 0644)

	loader := &ConfigurationLoader{FilenameToChecks: []string{tempFile.Name()}}
	cfg, err := loader.Loads(&Configuration{})

	assert.NoError(t, err)
	assert.Equal(t, []string{".inc", ".module"}, cfg.Extensions["php"])
	assert.Equal(t, []string{".go2"}, cfg.Extensions["go"])

	// GetExtensionsForLanguage should merge with defaults
	phpExts := cfg.GetExtensionsForLanguage("php")
	assert.Contains(t, phpExts, ".php")
	assert.Contains(t, phpExts, ".inc")
	assert.Contains(t, phpExts, ".module")
}

func TestCreateDefaultFile(t *testing.T) {
	loader := NewConfigurationLoader()

	err := loader.CreateDefaultFile()
	defer os.Remove(".ast-metrics.yaml")
	assert.NoError(t, err)

	// Check if the file was created
	_, err = os.Stat(".ast-metrics.yaml")
	assert.NoError(t, err)

	// Check if the file is parsable
	cfg, err := loader.Loads(&Configuration{})
	assert.NoError(t, err)
	assert.NotNil(t, cfg)

	// The generated file must use field names that actually map onto the
	// configuration schema, otherwise the requirements are silently ignored.
	assert.NotNil(t, cfg.Requirements)
	assert.NotNil(t, cfg.Requirements.Rules)
	assert.NotNil(t, cfg.Requirements.Rules.Volume)
	assert.NotNil(t, cfg.Requirements.Rules.Volume.Loc)
	assert.Equal(t, 100, *cfg.Requirements.Rules.Volume.Loc)
	assert.NotNil(t, cfg.Requirements.Rules.Complexity)
	assert.NotNil(t, cfg.Requirements.Rules.Complexity.Cyclomatic)
	assert.Equal(t, 10, *cfg.Requirements.Rules.Complexity.Cyclomatic)
}

func TestLoadsTheJsonFunctionsOption(t *testing.T) {
	tempFile, _ := ioutil.TempFile("", "test")
	defer os.Remove(tempFile.Name())
	tempFile.Write([]byte("reports:\n  json: ./build/report.json\n  json_functions: true\n"))
	tempFile.Close()

	cfg, err := (&ConfigurationLoader{FilenameToChecks: []string{tempFile.Name()}}).Loads(&Configuration{})

	assert.Nil(t, err)
	assert.True(t, cfg.Reports.JsonFunctions)
}
