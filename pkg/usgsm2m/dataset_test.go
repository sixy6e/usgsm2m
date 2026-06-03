package usgsm2m

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestDatasetFilters_Resilience ensures that fields typed as interface{}
// (like legacyFieldId) can unmarshal varied JSON types without throwing errors.
func TestDatasetFilters_Resilience(t *testing.T) {
	// simulated raw response containing tricky variant types (null vs integer vs string)
	mockJSONResponse := `[
		{
			"id": "5e83d120a1f26",
			"legacyFieldId": null,
			"dictionaryLink": "https://example.com",
			"fieldLabel": "WRS Path",
			"searchSql": "wrs_path",
			"fieldConfig": {
				"type": "Text",
				"numElements": null
			}
		},
		{
			"id": "5e83d120b9e43",
			"legacyFieldId": 10042,
			"dictionaryLink": "https://example.com",
			"fieldLabel": "WRS Row",
			"searchSql": "wrs_row",
			"fieldConfig": {
				"type": "Range",
				"numElements": "2"
			}
		}
	]`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// emulate what doRequest receives from the envelope mapping
		w.Write([]byte(`{"data": ` + mockJSONResponse + `}`))
	}))
	defer server.Close()

	client, _ := NewClient("test", "test", 1, "./tmp", WithLogger(nil))
	client.baseURL = server.URL

	filters, err := client.Request.FetchDatasetFilters(context.Background(), "landsat_ot_c2_l1")
	if err != nil {
		t.Fatalf("Failed to decode dynamic dataset filters: %v", err)
	}

	if len(filters) != 2 {
		t.Fatalf("Expected 2 filters parsed, got %d", len(filters))
	}

	// verify our interface{} handles null safely
	if filters[0].LegacyFieldID != nil {
		t.Errorf("Expected legacyFieldId to be nil for the first item, got %v", filters[0].LegacyFieldID)
	}

	// verify our interface{} handles integers safely
	if val, ok := filters[1].LegacyFieldID.(float64); !ok || val != 10042 {
		// note: encoding/json unmarshals generic interface{} numbers as float64
		t.Errorf("Expected legacyFieldId to be 10042, got %v", filters[1].LegacyFieldID)
	}
}

// TestDataset_NullPointerSafety checks that the explicit pointer types (*string)
// effectively capture null properties without crashing the engine.
func TestDataset_NullPointerSafety(t *testing.T) {
	mockDatasetJSON := `{
		"abstractText": null,
		"acquisitionEnd": null,
		"acquisitionStart": "1972-07-23",
		"allowInKmz": false,
		"catalogs": ["EE"],
		"collectionLongName": "Landsat 1-5 MSS C2 L1",
		"collectionName": "LANDSAT_MSS_C2_L1",
		"dataOwner": "USGS",
		"datasetAlias": null,
		"datasetCategoryName": "Landsat",
		"datasetId": "landsat_mss_c2_l1",
		"dateUpdated": "2020-12-30",
		"doiNumber": "https://doi.org/10.5066/F7AM1B8N",
		"ingestFrequency": "None",
		"keywords": "landsat mss",
		"legacyId": null,
		"sceneCount": 1345000,
		"supportCloudCover": true,
		"supportDeletionSearch": false,
		"temporalCoverage": "1972-07-23 to 1999-10-31"
	}`

	var ds Dataset
	err := json.Unmarshal([]byte(mockDatasetJSON), &ds)
	if err != nil {
		t.Fatalf("Strict structural unmarshal failed: %v", err)
	}

	// assert that pointers are safely extracted as nil rather than zero-valued strings
	if ds.AbstractText != nil {
		t.Errorf("Expected AbstractText pointer to be nil, got %v", *ds.AbstractText)
	}

	if ds.DoiNumber == nil || *ds.DoiNumber != "https://doi.org/10.5066/F7AM1B8N" {
		t.Errorf("Expected valid DoiNumber pointer string extraction")
	}
}
