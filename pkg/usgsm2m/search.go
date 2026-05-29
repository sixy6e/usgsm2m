package usgsm2m

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/paulmach/orb/geojson"
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

// AcquisitionFilter handles date ranges
type AcquisitionFilter struct {
	// The date the scene began acquisition - ISO 8601 Formatted Date
	Start string `json:"start,omitempty"` // Use string for "YYYY-MM-DD"
	// The date the scene ended acquisition - ISO 8601 Formatted Date
	End string `json:"end,omitempty"`
}

// CloudCoverFilter handles filtering of acquisitions by cloud coverage
type CloudCoverFilter struct {
	// Used to limit results by minimum cloud cover (for supported datasets)
	Min int64 `json:"min,omitempty"`
	// Used to limit results by maximum cloud cover (for supported datasets)
	Max int64 `json:"max,omitempty"`
	// Used to determine if scenes with unknown cloud cover values should be included in the results
	IncludeUnknown bool `json:"includeUnknown,omitempty"`
}

type MetadataFilter struct {
	FilterType string      `json:"filterType"`
	FilterId   string      `json:"filterId,omitempty"`
	Value      interface{} `json:"value,omitempty"`
	// For BETWEEN
	FirstValue interface{} `json:"firstValue,omitempty"`
	// For BETWEEN
	SecondValue interface{} `json:"secondValue,omitempty"`
	Operand     string      `json:"operand,omitempty"`
	// For AND/OR
	Filters []MetadataFilter `json:"childFilters,omitempty"`
}

// MetadataOption defines the function signature for our options
type MetadataOption func(*MetadataFilter)
type SpatialOption func(*SpatialFilter)

type IngestFilter struct {
	// Used to filter scenes by first metadata ingest
	Start string `json:"start,omitempty"`
	// Used to filter scenes by last metadata ingest
	End string `json:"end,omitempty"`
}

// SpatialFilter handles the abstract M2M data model.
// By using pointers with omitempty, we can toggle between MBR and GeoJSON seamlessly.
type SpatialFilter struct {
	// Must be "mbr" or "geojson"
	FilterType string `json:"filterType"`

	// MBR (Minimum Bounding Box) fields - explicit lower-case 'c' matching M2M specs
	LowerLeft  *Coordinate `json:"lowerLeft,omitempty"`
	UpperRight *Coordinate `json:"upperRight,omitempty"`

	// GeoJSON field - ready to accept raw bytes from paulmach/orb/geojson
	GeoJson *json.RawMessage `json:"geoJson,omitempty"`
}

type SceneFilter struct {
	// Dataset name
	DatasetName string `json:"datasetName,omitempty"`
	// Used to apply a acquisition filter on the data
	Acquisition *AcquisitionFilter `json:"acquisitionFilter,omitempty"`
	// Used to apply a cloud cover filter on the data
	CloudCover *CloudCoverFilter `json:"cloudCoverFilter,omitempty"`
	// Used to apply a metadata filter on the data
	Metadata *MetadataFilter `json:"metadataFilter,omitempty"`
	// Used to apply a spatial filter on the data
	Spatial *SpatialFilter `json:"spatialFilter,omitempty"`
	// Used to apply month numbers from 1 to 12 on the data
	SeasonalFilter []int64 `json:"seasonalFilter,omitempty"`
}

// NewMetadataFilter is the single entry point using functional options
func NewMetadataFilter(opts ...MetadataOption) MetadataFilter {
	f := &MetadataFilter{}
	for _, opt := range opts {
		opt(f)
	}
	return *f
}

// WithValue sets the VALUE type and its fields
func WithValue(id string, val interface{}, operand string) MetadataOption {
	return func(f *MetadataFilter) {
		f.FilterType = "value"
		f.FilterId = id
		f.Value = val
		f.Operand = operand
	}
}

// WithAnd nests other filters in an AND block
func WithAnd(filters ...MetadataFilter) MetadataOption {
	return func(f *MetadataFilter) {
		f.FilterType = "and"
		f.Filters = filters
	}
}

// WithBetween sets up a range constraint on a specific metadata field ID
func WithBetween(id string, first interface{}, second interface{}) MetadataOption {
	return func(f *MetadataFilter) {
		f.FilterType = "between"
		f.FilterId = id
		f.FirstValue = first
		f.SecondValue = second
	}
}

// NewMbrFilter builds a bounding box spatial filter
func NewMbrFilter(minLat, minLon, maxLat, maxLon float64) SpatialFilter {
	return SpatialFilter{
		FilterType: "mbr",
		LowerLeft:  &Coordinate{Longitude: minLon, Latitude: minLat},
		UpperRight: &Coordinate{Longitude: maxLon, Latitude: maxLat},
	}
}

// NewSpatialFilter follows the options pattern. But as this is
// a single option, is merely a generic constructor.
// Keeping around both patterns to see which is more suitable
// in the long run.
func NewSpatialFilter(opt SpatialOption) *SpatialFilter {
	f := &SpatialFilter{}
	opt(f)
	return f
}

// WithMbr sets the minimum bounding rectangle filter
func WithMbr(minLat, minLon, maxLat, maxLon float64) SpatialOption {
	return func(f *SpatialFilter) {
		f.FilterType = "mbr"
		f.LowerLeft = &Coordinate{Longitude: minLon, Latitude: minLat}
		f.UpperRight = &Coordinate{Longitude: maxLon, Latitude: maxLat}
	}
}

// WithGeoJSONGeometry takes a pre-wrapped geojson.Geometry container (e.g., from ParseGeoJSONFile)
// and marshals it directly into the SpatialFilter payload.
func WithGeoJSONGeometry(geom *geojson.Geometry) SpatialOption {
	return func(f *SpatialFilter) {
		f.FilterType = "geojson"

		bytes, _ := geom.MarshalJSON()

		raw := json.RawMessage(bytes)
		f.GeoJson = &raw
	}
}

type TemporalFilter struct {
	// ISO 8601 Formatted Date
	// Start time.Time `json:"start"`
	Start string `json:"start"`
	// ISO 8601 Formatted Date
	// End time.Time `json:"end"`
	End string `json:"end"`
}

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
