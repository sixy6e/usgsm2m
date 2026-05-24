package usgsm2m

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/paulmach/orb"
	"github.com/paulmach/orb/geojson"
)

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

// Point represents a single GeoJSON coordinate [longitude, latitude]
type Point []float64

type GeoJson struct {
	Type        string    `json:"type"`        // Always "Polygon" for this case
	Coordinates [][]Point `json:"coordinates"` // Array of rings containing points
}

// NewCoordinate constructs a Coordinate containing longitude and latitude coordinates
func NewCoordinate(lon float64, lat float64) Coordinate {
	return Coordinate{Longitude: lon, Latitude: lat}
}

// ToPoint converts a Coordinate type to a Point type
func (c *Coordinate) ToPoint() (pnt Point) {
	pnt = []float64{c.Longitude, c.Latitude}
	return pnt
}

// NewSimplePolygon converts a slice of our Coordinate structs
// into the deeply nested GeoJSON format the USGS expects.
func NewSimplePolygon(coords []Coordinate) GeoJson {
	ring := make([]Point, len(coords))
	for i, c := range coords {
		// GeoJSON is STRICTLY [longitude, latitude]
		ring[i] = c.ToPoint()
	}

	return GeoJson{
		Type: "Polygon",
		// wrap the ring in one more slice to satisfy the "array of rings" requirement
		Coordinates: [][]Point{ring},
	}
}

// NewSimplePolygonClosed converts a slice of Coordinates into the deeply nested
// GeoJSON format and ensures the ring is "closed" as required by the USGS.
func NewSimplePolygonClosed(coords []Coordinate) GeoJson {
	if len(coords) == 0 {
		return GeoJson{Type: "Polygon"}
	}

	// prepare the ring (the slice of points)
	ring := make([]Point, 0, len(coords)+1)
	for _, c := range coords {
		ring = append(ring, c.ToPoint())
	}

	// "Close the Loop" logic
	// A valid GeoJSON polygon must start and end at the same point.
	first := ring[0]
	last := ring[len(ring)-1]

	if first[0] != last[0] || first[1] != last[1] {
		// if they don't match, append the first point to the end
		ring = append(ring, first)
	}

	return GeoJson{
		Type: "Polygon",
		// USGS expects an array of rings (the first being the exterior)
		Coordinates: [][]Point{ring},
	}
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

// This allows doRequest to handle error checking generically.
type Response interface {
	GetBase() *BaseResponse
}

type BaseResponse struct {
	Version      string  `json:"version"`
	ErrorCode    *string `json:"errorCode"`
	ErrorMessage string  `json:"errorMessage"`
	RequestId    int64   `json:"requestId"`
	SessionId    *int64  `json:"sessionId"`
}

// Original label as defined by M2M docs.
// type DownloadResponse struct {
// 	Available          bool               `json:"available"`
// 	BulkAvailable      bool               `json:"bulkAvailable"`
// 	DatasetId          string             `json:"datasetId"`
// 	DisplayId          string             `json:"displayId"`
// 	DownloadName       string             `json:"downloadName"`
// 	DownloadSystem     string             `json:"downloadSystem"`
// 	EntityId           string             `json:"entityId"`
// 	Filesize           int64              `json:"filesize"`
// 	Id                 string             `json:"id"`
// 	ProductCode        string             `json:"productCode"`
// 	ProductName        string             `json:"productName"`
// 	SecondaryDownloads []DownloadResponse `json:"secondaryDownloads"`
// }

type DownloadOption struct {
	Available      bool   `json:"available"`
	BulkAvailable  bool   `json:"bulkAvailable"`
	DatasetId      string `json:"datasetId"`
	DisplayId      string `json:"displayId"`
	DownloadName   string `json:"downloadName"`
	DownloadSystem string `json:"downloadSystem"`
	EntityId       string `json:"entityId"`
	Filesize       int64  `json:"filesize"`
	Id             string `json:"id"`
	ProductCode    string `json:"productCode"`
	ProductName    string `json:"productName"`
	// Recursive: some products have sub-products
	SecondaryDownloads []DownloadOption `json:"secondaryDownloads"`
}

// DuplicateProducts on the M2M side is polymorphic. When nothing M2M returns an
// empty array. When populated M2M returns a map[string]string.
type DuplicateProducts map[string]string

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

// DownloadResult is the core "package" of files returned by Request or Retrieve
type DownloadRequestResult struct {
	Available         []AvailableDownload `json:"availableDownloads"`
	Preparing         []PreparingDownload `json:"preparingDownloads"`
	Failed            []FailedDownload    `json:"failed"`
	DuplicateProducts DuplicateProducts   `json:"duplicateProducts"`
}

// DownloadRequestResponse is the "Receipt" from the download-request endpoint
type DownloadRequestResponse struct {
	BaseResponse
	RequestId int64                 `json:"requestId"`
	Data      DownloadRequestResult `json:"data"`
}

// AvailableDownload represents a file that has a URL ready for the worker pool
type AvailableDownload struct {
	DownloadId  int64  `json:"downloadId"`
	EntityId    string `json:"entityId"`
	Url         string `json:"url"`
	FileSize    int64  `json:"filesize"`
	ProductName string `json:"productName"` // Keep this for logging/validation
}

// PreparingDownload represents a file the USGS is currently fetching from tape/archive
type PreparingDownload struct {
	DownloadId int64  `json:"downloadId"`
	EntityId   string `json:"entityId"`
}

type FailedDownload struct {
	DownloadId   int64  `json:"downloadId"`
	EntityId     string `json:"entityId"`
	ErrorMessage string `json:"errorMessage"` // sometimes 'error' or 'message'
}

// OLDER struct logic, has been reworked
// type SceneSearchRequest struct {
// 	DatasetName string            `json:"datasetName"`
// 	SceneFilter map[string]interface{} `json:"sceneFilter,omitempty"`
// 	MaxResults  int               `json:"maxResults,omitempty"`
// 	StartingNumber int            `json:"startingNumber,omitempty"`
// }

// OLDER struct logic, has been reworked
// type SceneSearchResult struct {
// 	EntityId  string `json:"entityId"`
// 	DisplayId string `json:"displayId"`
// 	// add more metadata fields here as needed
// }

// OLDER struct logic, has been reworked
// type SceneSearchResponse struct {
// 	BaseResponse
// 	Data struct {
// 		Results []SceneSearchResult `json:"results"`
// 	} `json:"data"`
// }

type DownloadRequestItem struct {
	ProductId string `json:"productId"`
	EntityId  string `json:"entityId"`
}

type DownloadRequestPayload struct {
	Downloads []DownloadRequestItem `json:"downloads"`
	Label     string                `json:"label,omitempty"` // Optional: name your "order"
}

// DownloadRequestResult will contain the response from the "download-request" method
// type DownloadRequestResult struct {
// 	Available         []AvailableDownload `json:"availableDownloads"`
// 	Preparing         []PreparingDownload `json:"preparingDownloads"`
// 	Failed            []FailedDownload    `json:"failedDownloads"`
// 	DuplicateProducts []string            `json:"duplicateProducts"`
// }

// RequestedDownload represents a file that the USGS is still preparing
type RequestedDownload struct {
	DownloadId int64  `json:"downloadId"`
	EntityId   string `json:"entityId"`
}

// DownloadItem refers to a single downloadable item
type DownloadItem struct {
	DownloadId int64  `json:"downloadId"`
	EntityId   string `json:"entityId"`
	URL        string `json:"url,omitempty"` // URL is empty if still preparing
}

// DownloadRetrievePayload defines the filtering criteria for fetching staged download links.
// Maps to the USGS M2M 'download-retrieve' request endpoint.
type DownloadRetrievePayload struct {
	DownloadApplication string `json:"downloadApplication,omitempty"`
	Label               string `json:"label,omitempty"`
}

type DownloadRetrieveResult struct {
	Available []AvailableDownload `json:"available"`
	Requested []PreparingDownload `json:"requested"`
	QueueSize int64               `json:"queueSize"`
	Eulas     []Eula              `json:"eulas"`
}

type DownloadRetrieveResponse struct {
	BaseResponse
	Data DownloadRetrieveResult `json:"data"`
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

// type Datasets struct {
// 	Datasets []Dataset
// }

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

// Names returns a flat slice of dataset display names/aliases
// func (d Datasets) Names() []string {
// 	return lo.Map(d.Datasets, func(ds Dataset, _ int) string {
// 		return ds.DatasetAlias // or ds.DatasetName depending on USGS field
// 	})
// }

type DownloadJob struct {
	EntityId    string
	ProductName string
	URL         string
}

type SceneListAddResponse struct {
	BaseResponse
	// The USGS returns the number of scenes successfully added in the "data" field
	Count int `json:"data"`
}

type DatasetMetadataRequest struct {
	DatasetName string `json:"datasetName"`
}

type DatasetMetadataResponse struct {
	BaseResponse
	Data map[string][]M2MFieldID `json:"data"` // potentially each dataset could have different fields
}

type M2MFieldID struct {
	ID        string `json:"id"`
	FieldName string `json:"field_name"`
}
