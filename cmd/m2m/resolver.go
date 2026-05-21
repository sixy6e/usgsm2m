package main

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/sixy6e/usgsm2m/pkg/usgsm2m"
)

// FieldResolver manages an in-memory map cache to shield the live USGS
// endpoints from duplicate requests during an execution block.
type FieldResolver struct {
	client *usgsm2m.Client
	mu     sync.RWMutex
	// Cache structure: datasetName -> fieldLabel -> fieldID
	cache map[string]map[string]string
}

// NewFieldResolver allocates a new synchronized cache coordinator instance
func NewFieldResolver(client *usgsm2m.Client) *FieldResolver {
	return &FieldResolver{
		client: client,
		cache:  make(map[string]map[string]string),
	}
}

// Resolve looks up a friendly label (like "WRS Path") and converts it to a
// physical system ID string, leveraging cache hits when available.
func (r *FieldResolver) Resolve(ctx context.Context, dataset string, label string) (string, error) {
	// Read-lock to verify if this dataset and field label exist in memory
	r.mu.RLock()
	datasetCache, datasetExists := r.cache[dataset]
	if datasetExists {
		if id, labelExists := datasetCache[strings.ToLower(label)]; labelExists {
			r.mu.RUnlock()
			return id, nil
		}
	}
	r.mu.RUnlock()

	// Upgrade to a write-lock to populate cache on miss
	r.mu.Lock()
	defer r.mu.Unlock()

	// double-check condition state in case another thread caught it during lock swap
	if _, datasetExists = r.cache[dataset]; !datasetExists {
		r.cache[dataset] = make(map[string]string)
	}

	// fetch fresh attributes from your newly integrated endpoint method!
	fields, err := r.client.FetchDatasetFilters(ctx, dataset)
	if err != nil {
		return "", fmt.Errorf("failed fetching field metadata for mapping evaluation: %w", err)
	}

	// ingest and normalise everything into dictionary layout
	for _, field := range fields {
		// normalising to lowercase makes user terminal lookups case-insensitive
		normLabel := strings.ToLower(field.FieldLabel)
		r.cache[dataset][normLabel] = field.ID
	}

	// attempt final lookup extraction from populated index map
	id, cleanHit := r.cache[dataset][strings.ToLower(label)]
	if !cleanHit {
		return "", fmt.Errorf("metadata search field '%s' does not exist on dataset '%s'", label, dataset)
	}

	return id, nil
}
