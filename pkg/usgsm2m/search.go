package usgsm2m

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/paulmach/orb"
	"github.com/paulmach/orb/geojson"
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

type SceneSearchData struct {
	Results         []Scene `json:"results"`
	TotalHits       int64   `json:"totalHits"`
	NextRecord      int64   `json:"nextRecord"`
	StartingNumber  int64   `json:"startingNumber"`
	RecordsReturned int64   `json:"recordsReturned"`
	NumExcluded     int64   `json:"numExcluded"`
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

type Coordinate struct {
	// Decimal degree coordinate in EPSG:4326 projection
	Longitude float64 `json:"longitude"`
	// Decimal degree coordinate in EPSG:4326 projection
	Latitude float64 `json:"latitude"`
}

type DateRange struct {
	// Expects an ISO 8601 formatted date.
	// Potentially could use time.Time, but will need a custom MarshalJSON
	// Used to apply a temporal filter on the data - ISO 8601 Formatted Date
	StartDate string `json:"startDate,omitempty"`
	// Used to apply a temporal filter on the data - ISO 8601 Formatted Date
	EndDate string `json:"endDate,omitempty"`
}

type Eula struct {
	// eulaCode is only populated when loading download orders
	EulaCode string `json:"eulaCode"`

	// agreementContent contains the actual legal clauses
	AgreementContent string `json:"agreementContent"`
}

type SceneMetadataConfig struct {
	// Used to include or exclude null values
	IncludeNulls bool `json:"includeNulls"`
	// Value can be 'full', 'summary' or null
	Type string `json:"type"`
	// Metadata template
	Template string `json:"template"`
}

type SpatialBoundsMbr struct {
	// the docs indicate a string not float64???
	// Decimal degree coordinate value in EPSG:4326 projection representing the northern most point of the MBR
	North float64 `json:"north"`
	// Decimal degree coordinate value in EPSG:4326 projection representing the northern most point of the MBR
	East float64 `json:"east"`
	// Decimal degree coordinate value in EPSG:4326 projection representing the northern most point of the MBR
	South float64 `json:"south"`
	// Decimal degree coordinate value in EPSG:4326 projection representing the northern most point of the MBR
	West float64 `json:"west"`
}

// SpatialBounds handles the polymorphic USGS abstract data model.
// It gracefully captures either a standard GeoJSON Polygon or an MBR.
// It uses paulmach/orb under the hood for type-safe GeoJSON processing.
type SpatialBounds struct {
	// Standard GeoJSON fields
	Type string `json:"type,omitempty"`
	// Coordinates [][]Point `json:"coordinates,omitempty"`
	// Coordinates interface{} `json:"coordinates,omitempty"`

	// High-fidelity GeoJSON data parsed cleanly via orb
	Geometry orb.Geometry

	// Minimum Bounding Rectangle (MBR) fields
	North float64 `json:"north,omitempty"`
	East  float64 `json:"east,omitempty"`
	South float64 `json:"south,omitempty"`
	West  float64 `json:"west,omitempty"`
}

type UserContext struct {
	// Internal user Identifier
	ContactId string `json:"contactId"`
	// Ip address used to send the request
	IpAddress string `json:"ipAddress"`
}

type TemporalCoverage struct {
	// possible only need yyyy-mm-dd formatting
	// Starting temporal extent of coverage - ISO 8601 Formatted Date
	StartDate time.Time
	// Ending temporal extent of the coverage - ISO 8601 Formatted Date
	EndDate time.Time
}

type Scene struct {
	EntityId   string   `json:"entityId"`
	DisplayId  string   `json:"displayId"`
	CloudCover *float64 `json:"cloudCover"` // using pointer because sample shows 'null'

	// Availability flags
	Options struct {
		Download bool `json:"download"`
		Bulk     bool `json:"bulk"`
		Order    bool `json:"order"`
	} `json:"options"`

	// Geometry
	SpatialBounds   SpatialBounds `json:"spatialBounds"`
	SpatialCoverage SpatialBounds `json:"spatialCoverage"`

	// Temporal info
	TemporalCoverage struct {
		StartDate string `json:"startDate"`
		EndDate   string `json:"endDate"`
	} `json:"temporalCoverage"`

	// Browse images
	Browse []struct {
		BrowsePath    string `json:"browsePath"`
		ThumbnailPath string `json:"thumbnailPath"`
	} `json:"browse"`
}

type Browse struct {
	Url string `json:"url"`
}

// DuplicateProducts on the M2M side is polymorphic. When nothing M2M returns an
// empty array. When populated M2M returns a map[string]string.
type DuplicateProducts map[string]string

// DownloadResult is the core "package" of files returned by Request or Retrieve
type DownloadRequestResult struct {
	Available         []AvailableDownload `json:"availableDownloads"`
	Preparing         []PreparingDownload `json:"preparingDownloads"`
	Failed            []FailedDownload    `json:"failed"`
	DuplicateProducts DuplicateProducts   `json:"duplicateProducts"`
}

type LogEntry struct {
	Timestamp string `json:"time"`
	Level     string `json:"level"`
	EntityId  string `json:"entity_id"`
	Filename  string `json:"filename,omitempty"`
	Bytes     int64  `json:"bytes,omitempty"`
	Message   string `json:"msg"`
	Error     string `json:"error,omitempty"`
}

// SceneListAddRequest contains the input payload for the scene-list-add endpoint
type SceneListAddRequest struct {
	// User defined name for the list
	ListId string `json:"listId"`
	// Dataset alias
	DatasetName string `json:"datasetName"`
	// Used to determine which ID is being used - entityId (default) or displayId
	IdField string `json:"idField"`

	// These allow the struct to handle both single and batch adds
	// Scene Identifier
	EntityId string `json:"entityId,omitempty"`
	// A list of Scene Identifiers
	EntityIds []string `json:"entityIds,omitempty"`

	// Defaults should be fine, but here if needed
	// User defined lifetime using ISO-8601 formatted duration (such as "P1M") for the list
	TimeToLive string `json:"timeToLive,omitempty"` // e.g., "PT1H"
	// Optional parameter to check download restricted access and availability
	CheckDownloadRestriction bool `json:"checkDownloadRestriction"`
}

type SceneListAddData struct {
	// The USGS returns the number of scenes successfully added in the "data" field
	Count int `json:"data"`
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

// SceneSearch executes a query to the M2M scene-search endpoint.
// If maxResults is > 0, it limits the total results. If maxResults <= 0, it drains the entire search result.
func (s *RequestService) SceneSearch(ctx context.Context, dataset string, filter *SceneFilter, maxResults int64) ([]Scene, error) {
	var allScenes []Scene
	var startingNumber int64 = 1
	var searchData SceneSearchData

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

		err := doRequest(ctx, s, "scene-search", reqBody, &searchData)
		if err != nil {
			return nil, err
		}

		// collate results
		allScenes = append(allScenes, searchData.Results...)

		// log current status
		s.client.logger.Info("Scene Search pagination status",
			"collected", len(allScenes),
			"total_hits", searchData.TotalHits,
		)

		// evaluation and termination checks
		if hasCeiling && int64(len(allScenes)) >= maxResults {
			break
		}

		// break if the API reports no further records exist or returned zero this turn
		if searchData.NextRecord <= 0 || searchData.RecordsReturned == 0 {
			break
		}

		// update the cursor index for the next HTTP call
		startingNumber = searchData.NextRecord
	}

	// final slice guard to ensure strict compliance with user ceiling limits
	if hasCeiling && int64(len(allScenes)) > maxResults {
		allScenes = allScenes[:maxResults]
	}

	return allScenes, nil
}

// We need a specific UnmarshalJSON as the M2M side is polymorphic.
// When nothing M2M returns an empty array.
// When populated M2M returns a map[string]string.
func (dp *DuplicateProducts) UnmarshalJSON(data []byte) error {
	if len(data) == 0 {
		return nil
	}

	// If the first non-space character is '[', it's an empty array response -> []
	trimmed := bytes.TrimSpace(data)
	if trimmed[0] == '[' {
		*dp = make(map[string]string)
		return nil
	}

	// Otherwise, it's a real object map -> {"entityId": "label"}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}

	*dp = m
	return nil
}

func (sb *SpatialBounds) UnmarshalJSON(data []byte) error {
	// Unmarshal into a map first to inspect whether it's an MBR or GeoJSON
	var probe map[string]any
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}

	// check if this is an MBR object (contains "north")
	if _, isMBR := probe["north"]; isMBR {
		type Alias SpatialBounds
		aux := (*Alias)(sb)
		return json.Unmarshal(data, aux)
	}

	// otherwise, treat it as a robust GeoJSON geometry block using orb
	geom, err := geojson.UnmarshalGeometry(data)
	if err != nil {
		return fmt.Errorf("failed to unmarshal spatial geometry via orb: %w", err)
	}

	if geom != nil {
		sb.Geometry = geom.Geometry()
	}

	return nil
}
