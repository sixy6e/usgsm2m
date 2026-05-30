package main

import (
	"encoding/json"
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

var fieldsCmd = &cobra.Command{
	Use:     "fields [dataset_name]",
	Aliases: []string{"dataset-metadata"},
	Short:   "List queryable metadata fields for a specific dataset",
	Long:    `Fetches the metadata profile from the USGS M2M API and lists the valid field names you can use for filtering.`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		datasetName := args[0]

		// sanity check for required credentials (reusing the shared 'cfg' layout)
		if cfg.Auth.Username == "" || cfg.Auth.Token == "" {
			return errors.New("missing authentication credentials; please set username and token in your .m2m.toml or use --username/--token flags")
		}

		// wrap context for clean Ctrl+C/SIGTERM tracking
		ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer stop()

		if !asJSON {
			logger.Info("Initializing USGS M2M Client for metadata lookup", "user", cfg.Auth.Username)
		}

		// instantiate the client
		client, err := usgsm2m.NewClient(
			cfg.Auth.Username,
			cfg.Auth.Token,
			1, // concurrency doesn't matter for metadata lookups
			cfg.Defaults.OutputDir,
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

		// execute the metadata fetch
		metadataProfile, err := client.Request.FetchDatasetMetadata(ctx, datasetName)
		if err != nil {
			return fmt.Errorf("failed to retrieve fields: %w", err)
		}

		// handle JSON output formatting
		if asJSON {
			jsonBytes, err := json.MarshalIndent(metadataProfile, "", "    ")
			if err != nil {
				return fmt.Errorf("failed to marshal metadata to JSON: %w", err)
			}
			fmt.Println(string(jsonBytes))
			return nil
		}

		// handle standard console table output
		exportFields, exists := metadataProfile["export"]
		if !exists {
			return fmt.Errorf("no user-queryable fields found under 'export' profile for dataset: %s", datasetName)
		}

		if len(exportFields) == 0 {
			cmd.Printf("Dataset '%s' has an empty export metadata profile.\n", datasetName)
			return nil
		}

		// alphabetise the table rows (ensures consistent print ordering every time)
		sort.Slice(exportFields, func(i, j int) bool {
			return strings.ToLower(exportFields[i].FieldName) < strings.ToLower(exportFields[j].FieldName)
		})

		cmd.Println("\nAvailable Search Fields:")
		cmd.Println("-------------------------")

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, "FIELD NAME\tSYSTEM ID")
		fmt.Fprintln(w, "----------\t---------")

		for _, f := range exportFields {
			fmt.Fprintf(w, "%s\t%s\n", f.FieldName, f.ID)
		}
		w.Flush()

		return nil
	},
}

func init() {
	rootCmd.AddCommand(fieldsCmd)
}
