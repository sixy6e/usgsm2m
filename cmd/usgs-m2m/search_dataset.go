package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/samber/lo"
	"github.com/sixy6e/usgsm2m/pkg/usgsm2m"
	"github.com/spf13/cobra"
)

// define command-scoped variables to tie into Cobra flags
var (
	datasetQuery   string
	catalogFlag    string
	categoryFlag   string
	publicOnlyFlag bool
	asJSONFlag     bool
)

var searchDatasetCmd = &cobra.Command{
	Use:   "dataset",
	Short: "Search available USGS M2M dataset collections",
	Long:  `Queries the M2M dataset-search endpoint to locate collections by name, category, or access levels.`,
	RunE:  runDatasetSearch,
}

func init() {
	// register local flags for the dataset subcommand
	searchDatasetCmd.Flags().StringVarP(&datasetQuery, "name", "n", "", "Filter datasets by name (automatic wildcards applied by M2M)")
	searchDatasetCmd.Flags().StringVar(&catalogFlag, "catalog", "", "Identify datasets associated with a given application catalog")
	searchDatasetCmd.Flags().StringVar(&categoryFlag, "category", "", "Restrict results to a specific category ID string")
	searchDatasetCmd.Flags().BoolVar(&publicOnlyFlag, "public", true, "Filter out datasets not accessible to the unauthenticated public")
	searchDatasetCmd.Flags().BoolVar(&asJSONFlag, "json", false, "Output raw dataset structures as formatted JSON")

	// add to parent search command
	searchCmd.AddCommand(searchDatasetCmd)
}

func runDatasetSearch(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// structural credentials check
	if cfg.Auth.Username == "" || cfg.Auth.Token == "" {
		return fmt.Errorf("missing authentication credentials; please set username and token")
	}

	// authenticate and initialise the client
	client, err := usgsm2m.NewClient(
		cfg.Auth.Username,
		cfg.Auth.Token,
		1,
		cfg.Defaults.OutputDir,
		usgsm2m.WithLogger(logger),
	)
	if err != nil {
		return fmt.Errorf("failed to initialise client: %w", err)
	}

	// ensure active session authorization header setup
	if err := client.Login(ctx); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	defer func() {
		if err := client.Logout(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Error cleaning up session: %v\n", err)
		}
	}()

	// assemble structural request body cleanly utilizing pointers or strings
	req := usgsm2m.DatasetSearchRequest{
		DatasetName: datasetQuery,
		Catalog:     catalogFlag,
		CategoryId:  categoryFlag,
		PublicOnly:  publicOnlyFlag,
	}

	logger.Info("Executing dataset catalog lookup...", "query", datasetQuery, "public_only", publicOnlyFlag)

	// execute
	datasets, err := client.Request.DatasetSearch(ctx, req)
	if err != nil {
		return fmt.Errorf("dataset search failed: %w", err)
	}

	// stylise outputs
	if asJSONFlag {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "    ")
		if err := encoder.Encode(datasets); err != nil {
			return fmt.Errorf("failed to encode datasets to JSON: %w", err)
		}
		return nil
	}

	if len(datasets) == 0 {
		logger.Info("No datasets matched your search parameters.")
		return nil
	}

	// pretty print tabular information
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "COLLECTION NAME\tDATASET ID\tSCENE COUNT\tTEMPORAL COVERAGE")
	fmt.Fprintln(w, "---------------\t----------\t-----------\t-----------------")

	for _, ds := range datasets {
		temporalStr := lo.FromPtrOr(ds.TemporalCoverage, "Unknown")

		fmt.Fprintf(w, "%s\t%s\t%d\t%s\n",
			ds.CollectionName,
			ds.DatasetId,
			ds.SceneCount,
			temporalStr,
		)
	}
	w.Flush()

	return nil
}
