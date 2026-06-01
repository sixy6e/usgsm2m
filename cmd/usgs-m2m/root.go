package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/sixy6e/usgsm2m/pkg/usgsm2m"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// global config available across all files in package main
var (
	cfg    usgsm2m.Config
	logger *slog.Logger
	asJSON bool

	// allows tests to use different viper configurations from env vars
	v *viper.Viper = viper.New()
)

var rootCmd = &cobra.Command{
	Use:   "usgs-m2m",
	Short: "usgs-m2m is a CLI tool for downloading USGS datasets like Landsat, MODIS, and VIIRS",
}

func init() {
	// initialize a text logger that outputs to standard error
	logger = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cobra.OnInitialize(func() {
		if err := initConfig(); err != nil {
			fmt.Fprintf(os.Stderr, "Config Error: %v\n", err)
			os.Exit(1)
		}
	})

	rootCmd.PersistentFlags().String("username", "", "USGS M2M Username")
	rootCmd.PersistentFlags().String("token", "", "USGS M2M API Token")

	rootCmd.PersistentFlags().BoolVarP(&asJSON, "json", "j", false, "Output command results in JSON format")

	v.BindPFlag("auth.username", rootCmd.PersistentFlags().Lookup("username"))
	v.BindPFlag("auth.token", rootCmd.PersistentFlags().Lookup("token"))
}

func initConfig() error {
	v.SetConfigName(".usgs-m2m")
	v.SetConfigType("toml")
	v.AddConfigPath("$HOME")
	v.AddConfigPath(".")
	v.AutomaticEnv()
	v.SetEnvPrefix("USGS_M2M")

	v.SetDefault("defaults.concurrency", 4)
	v.SetDefault("defaults.dataset", "landsat_ot_c2_l1")
	v.SetDefault("defaults.output_dir", "./downloads")

	if err := v.ReadInConfig(); err != nil {
		var configFileNotFoundError viper.ConfigFileNotFoundError
		if errors.As(err, &configFileNotFoundError) {
			// missing config is an acceptable path
			logger.Debug("No configuration file found (.usgs-m2m.toml); relying on defaults and environment variables")
		} else {
			// return a fatal error rather than crashing as previously done
			// log.Fatalf("Critical error reading configuration file: %v", err)
			return fmt.Errorf("critical error reading configuration file: %w", err)
		}
	}

	// if err := viper.Unmarshal(&cfg); err != nil {
	// 	log.Fatalf("Unable to decode config into struct: %v", err)
	// }
	if err := v.Unmarshal(&cfg); err != nil {
		return fmt.Errorf("unable to decode config into struct: %w", err)
	}
	return nil
}
