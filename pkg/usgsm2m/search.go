package usgsm2m

import (
	"context"
	"fmt"

	"github.com/samber/lo"
)

// SceneSearchRequest represents the payload for the 'scene-search' endpoint
type SceneSearchRequest struct {
	// Used to identify the dataset to search
	DatasetName string `json:"datasetName"`
	// Used to filter data within the dataset
	SceneFilter *SceneFilter `json:"sceneFilter,omitempty"`

	// Metadata for pagination and sorting
	// How many results should be returned ? (default = 100)
	MaxResults int64 `json:"maxResults,omitempty"`
	// Used to identify the start number to search from
	StartingNumber int64 `json:"startingNumber,omitempty"`
	// Determines which field to sort the results on
	SortField string `json:"sortField,omitempty"`
	// Determines how the results should be sorted - ASC or DESC
	SortDirection string `json:"sortDirection,omitempty"`
	// If populated, identifies which metadata to return (summary or full)
	MetadataType string `json:"metadataType,omitempty"`

	// Interactions with an existing scene-list
	// If provided, defined a scene-list listId to use to track scenes selected for comparison
	CompareListName string `json:"compareListName,omitempty"`
	// If provided, defined a scene-list listId to use to track scenes selected for bulk ordering
	BulkListName string `json:"bulkListName,omitempty"`
	// If provided, defined a scene-list listId to use to track scenes selected for on-demand ordering
	OrderListName string `json:"orderListName,omitempty"`
	// If provided, defined a scene-list listId to use to exclude scenes from the results
	ExcludeListName string `json:"excludeListName,omitempty"`
	// Optional parameter to include null metadata values
	IncludeNullMetadataValues bool `json:"includeNullMetadataValues,omitempty"`
}

type SceneSearchResponse struct {
	BaseResponse
	Data struct {
		Results         []Scene `json:"results"`
		TotalHits       int64   `json:"totalHits"`
		NextRecord      int64   `json:"nextRecord"`
		StartingNumber  int64   `json:"startingNumber"`
		RecordsReturned int64   `json:"recordsReturned"`
		NumExcluded     int64   `json:"numExcluded"`
	} `json:"data"`
}

type DatasetSearchRequest struct {
	// Used to identify datasets that are associated with a given application
	Catalog string `json:"catalog,omitempty"`
	// Used to restrict results to a specific category (does not search sub-sategories)
	CategoryId string `json:"categoryId,omitempty"`
	// Used as a filter with wildcards inserted at the beginning and the end of the supplied value
	DatasetName string `json:"datasetName,omitempty"`
	// Optional parameter to include messages regarding specific dataset components
	IncludeMessages bool `json:"includeMessages,omitempty"`
	// Used as a filter out datasets that are not accessible to unauthenticated general public users
	PublicOnly bool `json:"publicOnly,omitempty"`
	// Optional parameter to include datasets that do not support geographic searching
	IncludeUnknownSpatial bool `json:"includeUnknownSpatial,omitempty"`
	// Used to filter data based on data acquisition
	TemporalFilter *TemporalFilter `json:"temporalFilter,omitempty"`
	// Used to filter data based on data location
	SpatialFilter *SpatialFilter `json:"spatialFilter,omitempty"`
	// Defined the sorting as Ascending (ASC) or Descending (DESC) - default is ASC
	SortDirection string `json:"sortDirection,omitempty"`
	// Identifies which field should be used to sort datasets (shortName - default, longName, dastasetName, GloVis)
	SortField string `json:"sortField,omitempty"`
	// Optional parameter to indicate whether to use customization
	UseCustomization bool `json:"useCustomization,omitempty"`
}

type DatasetSearchResponse struct {
	BaseResponse
	Data []Dataset `json:"data"` // Dataset-search returns a simple array
}

// Names returns a flat slice of dataset display names/aliases
func (d DatasetSearchResponse) Names() []string {
	return lo.Map(d.Data, func(ds Dataset, _ int) string {
		return *ds.DatasetAlias // or ds.DatasetName depending on USGS field
	})
}

// SceneSearch executes a query to the M2M scene-search endpoint.
// If maxResults is > 0, it limits the total results. If maxResults <= 0, it drains the entire search result.
func (s *RequestService) SceneSearch(ctx context.Context, dataset string, filter *SceneFilter, maxResults int64) ([]Scene, error) {
	var allScenes []Scene
	var startingNumber int64 = 1

	// determine if the user set an explicit ceiling or wants an unlimited fetch
	hasCeiling := maxResults > 0
	const maxPageSize = 100 // USGS M2M optimal single-page batch size

	for {
		// calculate the pageSize for this chunk
		pageSize := int64(maxPageSize)

		if hasCeiling {
			remaining := maxResults - int64(len(allScenes))
			if remaining <= 0 {
				break // target ceiling met exactly
			}
			// clamp to remaining if we are approaching the limit
			if remaining < pageSize {
				pageSize = remaining
			}
		}

		reqBody := SceneSearchRequest{
			DatasetName:    dataset,
			SceneFilter:    filter,
			MaxResults:     pageSize,
			StartingNumber: startingNumber,
		}

		var response SceneSearchResponse
		err := s.doRequest(ctx, "scene-search", reqBody, &response)
		if err != nil {
			return nil, err
		}

		// collate results
		allScenes = append(allScenes, response.Data.Results...)

		// log current status
		s.client.logger.Info("Scene Search pagination status",
			"collected", len(allScenes),
			"total_hits", response.Data.TotalHits,
		)

		// evaluation and termination checks
		if hasCeiling && int64(len(allScenes)) >= maxResults {
			break
		}

		// break if the API reports no further records exist or returned zero this turn
		if response.Data.NextRecord <= 0 || response.Data.RecordsReturned == 0 {
			break
		}

		// update the cursor index for the next HTTP call
		startingNumber = response.Data.NextRecord
	}

	// final slice guard to ensure strict compliance with user ceiling limits
	if hasCeiling && int64(len(allScenes)) > maxResults {
		allScenes = allScenes[:maxResults]
	}

	return allScenes, nil
}

func (s *RequestService) DatasetSearch(ctx context.Context, req DatasetSearchRequest) (dss []Dataset, err error) {
	var resp DatasetSearchResponse
	err = s.doRequest(ctx, "dataset-search", req, &resp)
	if err != nil {
		return dss, err
	}
	dss = resp.Data
	return dss, nil
}

// ValidateDataset checks if the provided string is a real dataset
func (s *RequestService) ValidateDataset(ctx context.Context, name string) (bool, error) {
	req := DatasetSearchRequest{}
	dss, err := s.DatasetSearch(ctx, req)
	if err != nil {
		return false, fmt.Errorf("failed to fetch dataset list: %w", err)
	}
	return lo.Contains(DatasetNames(dss), name), nil
}
