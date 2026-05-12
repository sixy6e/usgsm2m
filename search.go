package usgsm2m

import (
	"context"
	"fmt"

	"github.com/samber/lo"
)

// GetBase allows BaseResponse to satisfy the Response interface.
func (b *BaseResponse) GetBase() *BaseResponse {
	return b
}

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

// HasError checks if the USGS API returned a functional error
func (b *BaseResponse) HasError() error {
	if b.ErrorCode != nil && *b.ErrorCode != "" {
		return fmt.Errorf("USGS Error (%s): %s", *b.ErrorCode, b.ErrorMessage)
	}
	return nil
}

func (s *RequestService) SceneSearch(ctx context.Context, dataset string, filter *SceneFilter, maxResults int64) ([]Scene, error) {
	reqBody := SceneSearchRequest{
		DatasetName: dataset,
		SceneFilter: filter,
		MaxResults:  maxResults,
	}

	var response SceneSearchResponse
	err := s.doRequest(ctx, "scene-search", reqBody, &response)
	if err != nil {
		return nil, err
	}

	return response.Data.Results, nil
}

func (s *RequestService) DatasetSearch(ctx context.Context, req DatasetSearchRequest) (ds Datasets, err error) {
	var resp DatasetSearchResponse
	err = s.doRequest(ctx, "dataset-search", req, &resp)
	if err != nil {
		return ds, err
	}
	ds = Datasets{Datasets: resp.Data}
	return ds, nil
}

// ValidateDataset checks if the provided string is a real dataset
func (s *RequestService) ValidateDataset(ctx context.Context, name string) (bool, error) {
	req := DatasetSearchRequest{}
	dss, err := s.DatasetSearch(ctx, req)
	if err != nil {
		return false, fmt.Errorf("failed to fetch dataset list: %w", err)
	}
	return lo.Contains(dss.Names(), name), nil
}
