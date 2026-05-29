package usgsm2m

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

// DownloadRequestResult will contain the response from the "download-request" method
// type DownloadRequestResult struct {
// 	Available         []AvailableDownload `json:"availableDownloads"`
// 	Preparing         []PreparingDownload `json:"preparingDownloads"`
// 	Failed            []FailedDownload    `json:"failedDownloads"`
// 	DuplicateProducts []string            `json:"duplicateProducts"`
// }

// type Datasets struct {
// 	Datasets []Dataset
// }

// Names returns a flat slice of dataset display names/aliases
// func (d Datasets) Names() []string {
// 	return lo.Map(d.Datasets, func(ds Dataset, _ int) string {
// 		return ds.DatasetAlias // or ds.DatasetName depending on USGS field
// 	})
// }
