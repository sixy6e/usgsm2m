package usgsm2m

import "time"

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
	LastValue interface{} `json:"lastValue,omitempty"`
	Operand   string      `json:"operand,omitempty"`
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

type SpatialFilter struct {
	FilterType string `json:"filterType"` // "mbr", "geojson", "point", etc.

	// MBR (Minimum Bounding Box) fields
	LowerLeft  *Coordinate `json:"lowerLeft,omitempty"`
	UpperRight *Coordinate `json:"upperRight,omitempty"`

	// GeoJSON fields
	GeoJson interface{} `json:"geoJson,omitempty"`

	// Point/Radius fields
	// Longitude *float64 `json:"longitude,omitempty"`
	// Latitude  *float64 `json:"latitude,omitempty"`
	// Distance  *float64 `json:"distance,omitempty"` // in meters
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

// NewMbrFilter builds a bounding box spatial filter
func NewMbrFilter(minLat, minLon, maxLat, maxLon float64) SpatialFilter {
	return SpatialFilter{
		FilterType: "mbr",
		LowerLeft:  &Coordinate{Longitude: minLon, Latitude: minLat},
		UpperRight: &Coordinate{Longitude: maxLon, Latitude: maxLat},
	}
}

// NewGeoJsonFilter builds a filter from a GeoJSON object
func NewGeoJsonFilter(data GeoJson) SpatialFilter {
	return SpatialFilter{
		FilterType: "geojson",
		GeoJson:    data,
	}
}

// NewSpatialFilter follows the options pattern. But as this is
// a single option, is merely a generic constructor.
// Keeping around both patterns to see which is more suitable
// in the long run.
func NewSpatialFilter(opt SpatialOption) SpatialFilter {
	f := &SpatialFilter{}
	opt(f)
	return *f
}

// WithMbr sets the minimum bounding rectangle filter
func WithMbr(minLat, minLon, maxLat, maxLon float64) SpatialOption {
	return func(f *SpatialFilter) {
		f.FilterType = "mbr"
		f.LowerLeft = &Coordinate{Longitude: minLon, Latitude: minLat}
		f.UpperRight = &Coordinate{Longitude: maxLon, Latitude: maxLat}
	}
}

// WithGeoJson sets the geojson filter
func WithGeoJson(data GeoJson) SpatialOption {
	return func(f *SpatialFilter) {
		f.FilterType = "geojson"
		f.GeoJson = data
	}
}

type TemporalFilter struct {
	// ISO 8601 Formatted Date
	Start time.Time `json:"start"`
	// ISO 8601 Formatted Date
	End time.Time `json:"end"`
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
