package main

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/sixy6e/usgsm2m/pkg/usgsm2m"
	"github.com/spf13/cobra"
)

var filtersCmd = &cobra.Command{
	Use:     "filters [dataset_name]",
	Aliases: []string{"dataset-filters"},
	Short:   "List searchable filter constraints for a dataset",
	Long:    `Fetches valid metadata fields from the dataset-filters endpoint for use in scene searching.`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		datasetName := args[0]

		// sanity check for required credentials (reusing the shared 'cfg' layout)
		if cfg.Username == "" || cfg.Token == "" {
			return errors.New("missing authentication credentials; please set username and token in your .m2m.toml or use --username/--token flags")
		}

		// wrap context for clean Ctrl+C/SIGTERM tracking
		ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer stop()

		if !asJSON {
			logger.Info("Initializing USGS M2M Client for metadata lookup", "user", cfg.Username)
		}

		// instantiate the client
		client, err := usgsm2m.NewClient(
			cfg.Username,
			cfg.Token,
			1, // concurrency doesn't matter for metadata lookups
			cfg.OutputDir,
			usgsm2m.WithLogger(logger),
		)
		if err != nil {
			return fmt.Errorf("failed to initialise client: %w", err)
		}

		// authenticate to obtain the active API key
		if !asJSON {
			logger.Info("Logging into USGS M2M service...")
		}
		if err := client.Login(ctx); err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		filterFields, err := client.FetchDatasetFilters(ctx, datasetName)
		if err != nil {
			return err
		}

		// sort by human-friendly label
		sort.Slice(filterFields, func(i, j int) bool {
			return strings.ToLower(filterFields[i].FieldLabel) < strings.ToLower(filterFields[j].FieldLabel)
		})

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, "FILTER LABEL\tFILTER ID\tTYPE")
		fmt.Fprintln(w, "------------\t---------\t----")
		for _, f := range filterFields {
			fmt.Fprintf(w, "%s\t%s\t%s\n", f.FieldLabel, f.ID, f.FieldConfig.Type)
		}
		w.Flush()
		return nil
	},
}

func init() {
	rootCmd.AddCommand(filtersCmd)
}
