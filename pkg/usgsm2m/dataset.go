package usgsm2m

import (
	"context"
	"fmt"
)

type DatasetMetadataRequest struct {
	DatasetName string `json:"datasetName"`
}

type M2MFieldID struct {
	ID        string `json:"id"`
	FieldName string `json:"field_name"`
}

type DatasetSearchData struct {
	Data []Dataset `json:"data"`
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
