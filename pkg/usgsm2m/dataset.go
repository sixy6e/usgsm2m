package usgsm2m

import (
	"context"
	"fmt"

	"github.com/samber/lo"
)

type DatasetMetadataRequest struct {
	DatasetName string `json:"datasetName"`
}

type M2MFieldID struct {
	ID        string `json:"id"`
	FieldName string `json:"field_name"`
}

// type DatasetSearchData struct {
// 	Data []Dataset `json:"data"`
// }

// DatasetFiltersResponse mirrors the top-level envelope returned by the 'dataset-filters' endpoint
type DatasetFiltersResponse struct {
	RequestID    int64                `json:"requestId"`
	Version      string               `json:"version"`
	Data         []DatasetFilterField `json:"data"`
	ErrorCode    string               `json:"errorCode,omitempty"`
	ErrorMessage string               `json:"errorMessage,omitempty"`
}

// DatasetFilterField represents an individual searchable filter definition asset template
type DatasetFilterField struct {
	ID             string            `json:"id"`
	LegacyFieldID  interface{}       `json:"legacyFieldId"` // Can be a null JSON block or integer
	DictionaryLink string            `json:"dictionaryLink"`
	FieldLabel     string            `json:"fieldLabel"`
	SearchSQL      string            `json:"searchSql"`
	FieldConfig    FieldConfigDetail `json:"fieldConfig"`
	ValueList      map[string]string `json:"valueList,omitempty"` // Option maps e.g., {"8": "8", "9": "9"}
}

// FieldConfigDetail holds validation rules and interactive UI properties from USGS
type FieldConfigDetail struct {
	Type          string      `json:"type"` // "Text", "Range", "Select"
	NumElements   interface{} `json:"numElements,omitempty"`
	DisplayListID string      `json:"displayListId,omitempty"`
}

type TemporalFilter struct {
	// ISO 8601 Formatted Date
	// Start time.Time `json:"start"`
	Start string `json:"start"`
	// ISO 8601 Formatted Date
	// End time.Time `json:"end"`
	End string `json:"end"`
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

type Dataset struct {
	// Pointers are being used to prevent unmarshal panics when USGS returns null
	AbstractText          *string        `json:"abstractText"`
	AcquisitionEnd        *string        `json:"acquisitionEnd"`
	AcquisitionStart      string         `json:"acquisitionStart"`
	AllowInKmz            bool           `json:"allowInKmz"`
	Catalogs              []string       `json:"catalogs"`
	CollectionLongName    string         `json:"collectionLongName"`
	CollectionName        string         `json:"collectionName"`
	DataOwner             string         `json:"dataOwner"`
	DatasetAlias          *string        `json:"datasetAlias"`
	DatasetCategoryName   string         `json:"datasetCategoryName"`
	DatasetId             string         `json:"datasetId"`
	DateUpdated           string         `json:"dateUpdated"`
	DoiNumber             *string        `json:"doiNumber"`
	IngestFrequency       string         `json:"ingestFrequency"`
	Keywords              string         `json:"keywords"`
	LegacyId              *int64         `json:"legacyId"`
	SceneCount            int64          `json:"sceneCount"`
	SpatialBounds         *SpatialBounds `json:"spatialBounds"`
	SupportCloudCover     bool           `json:"supportCloudCover"`
	SupportDeletionSearch bool           `json:"supportDeletionSearch"`
	TemporalCoverage      *string        `json:"temporalCoverage"`
}

// FetchDatasetMetadata uses the "dataset-metadata" endpoint to retrieve all metadata fields for a given dataset
func (s *RequestService) FetchDatasetMetadata(ctx context.Context, datasetName string) (map[string][]M2MFieldID, error) {
	req := DatasetMetadataRequest{
		DatasetName: datasetName,
	}

	var data map[string][]M2MFieldID

	err := doRequest(ctx, s, "dataset-metadata", req, &data)
	if err != nil {
		return nil, err
	}

	return data, nil
}

// FetchDatasetFilters retrieves searchable parameters and valid option mappings for scene queries
func (s *RequestService) FetchDatasetFilters(ctx context.Context, datasetName string) ([]DatasetFilterField, error) {
	// payload for the endpoint
	reqPayload := map[string]string{
		"datasetName": datasetName,
	}

	var data []DatasetFilterField

	// fire the network call
	err := doRequest(ctx, s, "dataset-filters", reqPayload, &data)
	if err != nil {
		return nil, fmt.Errorf("network execution failed for dataset-filters: %w", err)
	}

	return data, nil
}

func (s *RequestService) DatasetSearch(ctx context.Context, req DatasetSearchRequest) (dss []Dataset, err error) {
	var datasets []Dataset
	err = doRequest(ctx, s, "dataset-search", req, &datasets)
	if err != nil {
		return dss, err
	}
	return datasets, nil
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
