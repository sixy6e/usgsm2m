package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/sixy6e/usgsm2m/pkg/usgsm2m"
)

// MetadataInput represents a raw user filter parsed from CLI string arguments
type MetadataInput struct {
	FieldName string
	Operand   string
	Value     string

	// Range boundaries (populated when Operand == "BETWEEN")
	FirstValue  string
	SecondValue string
}

// parseMetaFlag converts strings like "WRS Path=90" or "WRS Path=90:95" into discrete query parts
func parseMetaFlag(raw string) (MetadataInput, error) {
	// Order matters: match multi-character operators before single ones if we add more later
	operators := []string{"=", "like"}

	for _, op := range operators {
		if idx := strings.Index(raw, op); idx != -1 {
			field := strings.TrimSpace(raw[:idx])
			val := strings.TrimSpace(raw[idx+len(op):])

			if field == "" || val == "" {
				return MetadataInput{}, fmt.Errorf("invalid meta filter format '%s' (missing field or value)", raw)
			}

			// --- Range Detection ---
			// If the parsed value contains a colon, upgrade this input to a BETWEEN operand
			if strings.Contains(val, ":") {
				valParts := strings.SplitN(val, ":", 2)
				return MetadataInput{
					FieldName:   field,
					Operand:     "between", // Dynamically swap operator context
					FirstValue:  strings.TrimSpace(valParts[0]),
					SecondValue: strings.TrimSpace(valParts[1]),
				}, nil
			}
			// ------------------------------------

			return MetadataInput{
				FieldName: field,
				Operand:   op,
				Value:     val,
			}, nil
		}
	}

	return MetadataInput{}, fmt.Errorf("no valid operator found in filter '%s' (supported: =, like)", raw)
}

// parseCloudFilter handles the cloud filter parsed from the CLI
func parseCloudFilter(input string) (*usgsm2m.CloudCoverFilter, error) {
	if input == "" {
		return nil, nil
	}

	var min, max int64
	var err error

	if strings.Contains(input, ":") {
		parts := strings.SplitN(input, ":", 2)
		min, err = strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid cloud min value: %w", err)
		}
		max, err = strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid cloud max value: %w", err)
		}
	} else {
		// treat a single number as a maximum ceiling (0 to max)
		min = 0
		max, err = strconv.ParseInt(strings.TrimSpace(input), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid cloud value: %w", err)
		}
	}

	if min < 0 || max > 100 || min > max {
		return nil, fmt.Errorf("cloud filter must be between 0 and 100, and min cannot exceed max")
	}

	return &usgsm2m.CloudCoverFilter{
		Min:            min,
		Max:            max,
		IncludeUnknown: true, // typically true so one doesn't miss unrated scenes, or map to a flag
	}, nil
}

// withClient encapsulates the repeated boilerplate of checking credentials,
// initializing the client, logging in, and safely deferring a session logout.
func withClient(ctx context.Context, action func(client *usgsm2m.Client) error) error {
	// sanity check for required authentication fields
	if cfg.Auth.Username == "" || cfg.Auth.Token == "" {
		return fmt.Errorf("missing authentication credentials; please set username and token")
	}

	// initialize the client
	client, err := usgsm2m.NewClient(
		cfg.Auth.Username,
		cfg.Auth.Token,
		1, // retries
		cfg.Defaults.OutputDir,
		usgsm2m.WithLogger(logger),
	)
	if err != nil {
		return fmt.Errorf("failed to initialise client: %w", err)
	}

	// authenticate
	if err := client.Login(ctx); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	// global guard rail: every command wrapped in this function automatically logs out
	defer func() {
		if err := client.Logout(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Error cleaning up session: %v\n", err)
		}
	}()

	// execute the actual CLI action payload
	return action(client)
}
