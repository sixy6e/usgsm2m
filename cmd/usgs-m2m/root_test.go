package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sixy6e/usgsm2m/pkg/usgsm2m"
	"github.com/spf13/viper"
)

func TestRootCmd_HelpOutput(t *testing.T) {
	v = viper.New()
	cfg = usgsm2m.Config{}

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"--help"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Unexpected execute failure: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "usgs-m2m is a CLI tool") {
		t.Errorf("Expected help text profile layout missing. Got:\n%s", output)
	}
}

func TestInitConfig_LoadingAndDefaults(t *testing.T) {
	t.Run("Falls back gracefully to default parameters when file is missing", func(t *testing.T) {
		v = viper.New()
		cfg = usgsm2m.Config{}

		// point to an empty, isolated temporary directory to guarantee a file-missing scenario
		v.AddConfigPath(t.TempDir())

		err := initConfig()
		if err != nil {
			t.Fatalf("Expected configuration layer to tolerate missing profile files, got: %v", err)
		}

		// use v.Unmarshal(&cfg) inside root.go.
		// to read defaults out of Viper's engine
		// registry when no file is found, read directly from the `v` global instance
		if v.GetInt("defaults.concurrency") != 4 {
			t.Errorf("Expected concurrency default 4, got %d", v.GetInt("defaults.concurrency"))
		}
		if v.GetString("defaults.output_dir") != "./downloads" {
			t.Errorf("Expected default output directory mapping missing, got %q", v.GetString("defaults.output_dir"))
		}
	})

	t.Run("Successfully loads parameters from file overriding defaults", func(t *testing.T) {
		tmpDir := t.TempDir()

		tomlContent := `
[auth]
username = "test_cl_user"
token = "secret_cli_token"

[defaults]
concurrency = 8
dataset = "modis_custom_v1"
`
		configPath := filepath.Join(tmpDir, ".usgs-m2m.toml")
		if err := os.WriteFile(configPath, []byte(tomlContent), 0644); err != nil {
			t.Fatalf("Failed to write mock config file: %v", err)
		}

		v = viper.New()
		v.AddConfigPath(tmpDir)
		v.SetConfigName(".usgs-m2m")
		v.SetConfigType("toml")
		cfg = usgsm2m.Config{}

		err := initConfig()
		if err != nil {
			t.Fatalf("Failed reading explicit config structure: %v", err)
		}

		if cfg.Auth.Username != "test_cl_user" {
			t.Errorf("Expected 'test_cl_user', got %q", cfg.Auth.Username)
		}
		if cfg.Defaults.Concurrency != 8 {
			t.Errorf("Expected config override value 8, got %d", cfg.Defaults.Concurrency)
		}
	})
}
