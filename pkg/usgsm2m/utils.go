package usgsm2m

import (
	"context"
	"fmt"
	"math/rand"
	"mime"
	"net/http"
	"os"
	"strings"

	"github.com/samber/lo"
)

// GetFilenameFromHeaders attempts to find the official filename in the response.
// If it fails, it falls back to a provided default name.
func GetFilenameFromHeaders(resp *http.Response, defaultName string) string {
	contentDisp := resp.Header.Get("Content-Disposition")
	if contentDisp == "" {
		return defaultName
	}

	// mime.ParseMediaType handles quotes and escaped characters automatically
	_, params, err := mime.ParseMediaType(contentDisp)
	if err != nil {
		return defaultName
	}

	if filename, ok := params["filename"]; ok {
		return filename
	}

	// fallback for extended parameter syntax
	if filename, ok := params["filename*"]; ok {
		return filename
	}

	return defaultName
}

// BatchSlice splits a slice of any type into a slice of slices of a specific size.
func BatchSlice[T any](items []T, batchSize int) [][]T {
	var batches [][]T
	for i := 0; i < len(items); i += batchSize {
		end := i + batchSize

		// ensure we don't go out of bounds on the last (shorter) batch
		if end > len(items) {
			end = len(items)
		}

		batches = append(batches, items[i:end])
	}
	return batches
}

// FileExists checks if a file or directory exists.
func FileExists(filename string) bool {
	info, err := os.Stat(filename)
	if err == nil {
		// ensure it's not an empty file
		return info.Size() > 0
	}
	return false
}

func GenerateBatchId() string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 6)
	for i := range b {
		// using math/rand is fine for a non-security identifier
		b[i] = charset[rand.Intn(len(charset))]
	}
	return fmt.Sprintf("batch-%s", string(b))
}

type FieldResolver struct {
	client *Client
	// Cache: map[datasetName]map[humanFieldName]fieldId
	cache map[string]map[string]string
}

func NewFieldResolver(c *Client) *FieldResolver {
	return &FieldResolver{
		client: c,
		cache:  make(map[string]map[string]string),
	}
}

// Resolve converts "WRS Path" to "5e81f1502c7f8da4" dynamically
func (r *FieldResolver) Resolve(ctx context.Context, dataset string, fieldName string) (string, error) {
	// check if we already fetched this dataset's definitions
	if fields, hit := r.cache[dataset]; hit {
		if id, found := fields[strings.ToLower(fieldName)]; found {
			return id, nil
		}
		return "", fmt.Errorf("field '%s' not valid for dataset %s", fieldName, dataset)
	}

	// cache miss: fetch directly from M2M API using the correct endpoint wrapper
	r.client.logger.Debug("Fetching dataset metadata fields from M2M API...", "dataset", dataset)

	metadataProfile, err := r.client.FetchDatasetMetadata(ctx, dataset)
	if err != nil {
		return "", fmt.Errorf("failed to fetch dataset metadata: %w", err)
	}

	// extract the 'export' category
	exportFields, exists := metadataProfile["export"]
	if !exists {
		return "", fmt.Errorf("metadata profile 'export' not found for dataset %s", dataset)
	}

	// populate local map using the correct JSON struct properties
	r.cache[dataset] = make(map[string]string)
	for _, f := range exportFields {
		// use f.FieldName and f.ID matching M2MFieldID struct definition!
		r.cache[dataset][strings.ToLower(f.FieldName)] = f.ID
	}

	// try looking it up one more time
	if id, found := r.cache[dataset][strings.ToLower(fieldName)]; found {
		return id, nil
	}
	return "", fmt.Errorf("field '%s' does not exist in dataset metadata", fieldName)
}

// DatasetNames extracts a flat slice of dataset alias names.
// Null returns from USGS will default to empty values eg ""
func DatasetNames(dss []Dataset) []string {
	return lo.Map(dss, func(ds Dataset, _ int) string {
		return lo.FromPtrOr(ds.DatasetAlias, "")
	})
}

// DatasetIDs extracts a slice of unique platform IDs.
func DatasetIDs(datasets []Dataset) []string {
	return lo.Map(datasets, func(ds Dataset, _ int) string {
		return ds.DatasetId
	})
}

// DatasetTemporalCoverage extracts a flat slice of dataset temporal coverages.
// Null returns from USGS will default to empty values eg ""
func DatasetTemporalCoverage(dss []Dataset) []string {
	return lo.Map(dss, func(ds Dataset, _ int) string {
		return lo.FromPtrOr(ds.TemporalCoverage, "Unknown")
	})
}
