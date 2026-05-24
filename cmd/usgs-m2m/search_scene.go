package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/sixy6e/usgsm2m/pkg/usgsm2m"
	"github.com/spf13/cobra"
)

var (
	metaFlags      []string
	limitFlag      int64
	startFlag      string
	endFlag        string
	cloudFlag      string
	searchSceneCmd = &cobra.Command{
		Use:   "scene",
		Short: "Search for scene IDs using human-readable metadata filters",
		Long:  `Queries the USGS M2M catalog by resolving friendly filter names (like WRS Path) into system IDs dynamically.`,
		RunE:  runSearchScene,
	}
)

func runSearchScene(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// structural credentials check
	if cfg.Auth.Username == "" || cfg.Auth.Token == "" {
		return fmt.Errorf("missing authentication credentials; please set username and token")
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	// authenticate and initialise the client
	logger.Info("Initializing client for scene search", "user", cfg.Auth.Username)
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

	logger.Info("Logging into USGS M2M service...")
	if err := client.Login(ctx); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	// process and parse raw terminal flag strings
	var parsedInputs []MetadataInput
	for _, rawFlag := range metaFlags {
		input, err := parseMetaFlag(rawFlag)
		if err != nil {
			return fmt.Errorf("flag parsing error: %w", err)
		}
		parsedInputs = append(parsedInputs, input)
	}

	// create the single resolver instance to benefit from the internal cache
	resolver := NewFieldResolver(client)
	apiFilter, err := BuildMetadataFilter(ctx, resolver, cfg.Defaults.Dataset, parsedInputs)
	if err != nil {
		return fmt.Errorf("failed to construct metadata filter: %w", err)
	}

	// initialise the base SceneFilter with the resolved metadata pointer
	sceneFilter := &usgsm2m.SceneFilter{
		Metadata: apiFilter,
	}

	// check for partial date inputs
	// USGS M2M might allow one empty side and insert either the earliest
	// for missing start or now if missing the end
	// but it's probably safer (and easier on the server) to not accidentally
	// request something unexpectedly
	if (startFlag != "" && endFlag == "") || (startFlag == "" && endFlag != "") {
		return fmt.Errorf("both --start and --end dates must be specified together to set a temporal window")
	}

	// if we pass this check and at least one is set, it means both are set
	if startFlag != "" {
		sceneFilter.Acquisition = &usgsm2m.AcquisitionFilter{
			Start: startFlag,
			End:   endFlag,
		}
	}

	cloudFilter, err := parseCloudFilter(cloudFlag)
	if err != nil {
		return fmt.Errorf("cloud filter error: %w", err)
	}

	sceneFilter.CloudCover = cloudFilter

	logger.Info("Executing scene search with metadata constraints...", "dataset", cfg.Defaults.Dataset, "filters", len(parsedInputs))

	results, err := client.Request.SceneSearch(ctx, cfg.Defaults.Dataset, sceneFilter, limitFlag)
	if err != nil {
		return fmt.Errorf("search failed: %w", err)
	}

	nResults := len(results)

	// output the discovered entity IDs
	if nResults == 0 {
		logger.Info("No scenes matched your search criteria.")
		return nil
	}

	entityIDs := make([]string, nResults)
	for i, scene := range results {
		entityIDs[i] = scene.EntityId
	}

	logger.Info("search results returned",
		"count", nResults,
		"max_results_limit", limitFlag,
	)

	if asJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "    ") // some nicer formatting

		output := struct {
			EntityIDs []string `json:"entity_ids"`
		}{
			EntityIDs: entityIDs,
		}

		if err := encoder.Encode(output); err != nil {
			return fmt.Errorf("failed to encode entity IDs to JSON: %w", err)
		}
	} else {
		for _, scene := range entityIDs {
			// using standard printed output so it can easily be redirected or piped
			// directly into the download tool or an ID text file
			fmt.Println(scene)
		}
	}

	return nil
}

func BuildMetadataFilter(ctx context.Context, resolver *FieldResolver, dataset string, inputs []MetadataInput) (*usgsm2m.MetadataFilter, error) {
	if len(inputs) == 0 {
		return nil, nil
	}

	var childFilters []usgsm2m.MetadataFilter

	for _, input := range inputs {
		// cache so we only query the live API *once* per dataset execution block
		fieldID, err := resolver.Resolve(ctx, dataset, input.FieldName)
		if err != nil {
			return nil, err
		}

		var filterItem usgsm2m.MetadataFilter

		// handle range requests vs. standard values
		if input.Operand == "between" {
			filterItem = usgsm2m.NewMetadataFilter(
				usgsm2m.WithBetween(fieldID, input.FirstValue, input.SecondValue),
			)
		} else {
			// maps input parameters directly to "VALUE" type blocks (e.g., EQUAL, LIKE)
			filterItem = usgsm2m.NewMetadataFilter(
				usgsm2m.WithValue(fieldID, input.Value, input.Operand),
			)
		}

		childFilters = append(childFilters, filterItem)
	}

	// if there's only one filter, there's no need to nest it inside an AND block
	if len(childFilters) == 1 {
		return &childFilters[0], nil
	}

	// group filter items inside the "AND" block container type
	finalFilter := usgsm2m.NewMetadataFilter(
		usgsm2m.WithAnd(childFilters...),
	)

	return &finalFilter, nil
}

func init() {
	// StringSliceVarP lets a user use -m multiple times in one execution
	searchSceneCmd.Flags().StringSliceVarP(&metaFlags, "meta", "m", []string{}, "Metadata filters to apply (e.g. -m 'WRS Path=90' -m 'WRS Row=32')")
	searchSceneCmd.Flags().StringP("dataset", "d", "landsat_ot_c2_l1", "The USGS dataset name")
	searchSceneCmd.Flags().Int64VarP(&limitFlag, "limit", "l", 100, "Maximum number of scenes to return")

	// acquisition date window flags
	searchSceneCmd.Flags().StringVar(&startFlag, "start", "", "Start date for scene acquisition (YYYY-MM-DD)")
	searchSceneCmd.Flags().StringVar(&endFlag, "end", "", "End date for scene acquisition (YYYY-MM-DD)")

	// cloud filtering
	searchSceneCmd.Flags().StringVar(&cloudFlag, "cloud", "", "Filter by cloud cover percentage (e.g., '15' for 0-15%, or '10:20')")

	// attach to parent search command
	searchCmd.AddCommand(searchSceneCmd)
}
