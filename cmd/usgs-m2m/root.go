package main

import (
	"errors"
	"log"
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
)

var rootCmd = &cobra.Command{
	Use:   "usgs-m2m",
	Short: "usgs-m2m is a CLI tool for downloading USGS datasets like Landsat, MODIS, and VIIRS",
}

func init() {
	// initialize a text logger that outputs to standard error
	logger = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().String("username", "", "USGS M2M Username")
	rootCmd.PersistentFlags().String("token", "", "USGS M2M API Token")

	rootCmd.PersistentFlags().BoolVarP(&asJSON, "json", "j", false, "Output command results in JSON format")

	viper.BindPFlag("username", rootCmd.PersistentFlags().Lookup("username"))
	viper.BindPFlag("token", rootCmd.PersistentFlags().Lookup("token"))
}

func initConfig() {
	viper.SetConfigName(".usgs-m2m")
	viper.SetConfigType("toml")
	viper.AddConfigPath("$HOME")
	viper.AddConfigPath(".")
	viper.AutomaticEnv()
	viper.SetEnvPrefix("USGS_M2M")

	viper.SetDefault("concurrency", 4)
	viper.SetDefault("dataset", "landsat_ot_c2_l1")
	viper.SetDefault("output_dir", "./downloads")

	if err := viper.ReadInConfig(); err != nil {
		var configFileNotFoundError viper.ConfigFileNotFoundError
		if errors.As(err, &configFileNotFoundError) {
			// missing config is an acceptable path
			logger.Debug("No configuration file found (.usgs-m2m.toml); relying on defaults and environment variables")
		} else {
			// crash if the file is there but unreadable or corrupted
			log.Fatalf("Critical error reading configuration file: %v", err)
		}
	}

	if err := viper.Unmarshal(&cfg); err != nil {
		log.Fatalf("Unable to decode config into struct: %v", err)
	}
}
