package usgsm2m

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSceneSearch_MetadataFilterCompilation(t *testing.T) {
	// create an isolated test logger that discards output to test output clean in the terminal
	testLogger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// define the "structural blueprint" for our test scenarios
	tests := []struct {
		name         string
		inputFilter  *SceneFilter
		validateJSON func(t *testing.T, payload map[string]interface{})
	}{
		{
			name: "Single Equality Filter (No AND nesting needed)",
			inputFilter: &SceneFilter{
				Metadata: &MetadataFilter{
					FilterType: "value",
					FilterId:   "5e83d120a1f26",
					Value:      "90",
					Operand:    "equals",
				},
			},
			validateJSON: func(t *testing.T, payload map[string]interface{}) {
				sf := payload["sceneFilter"].(map[string]interface{})
				meta := sf["metadataFilter"].(map[string]interface{})

				if meta["filterType"] != "value" {
					t.Errorf("Expected filterType 'value', got %v", meta["filterType"])
				}
				if meta["filterId"] != "5e83d120a1f26" {
					t.Errorf("Expected filterId '5e83d120a1f26', got %v", meta["filterId"])
				}
				if meta["value"] != "90" {
					t.Errorf("Expected value '90', got %v", meta["value"])
				}
			},
		},
		{
			name: "Multiple Filters (Nested AND container)",
			inputFilter: &SceneFilter{
				Metadata: &MetadataFilter{
					FilterType: "and",
					Filters: []MetadataFilter{ // field name: Filters, tag name: childFilters
						{FilterType: "value", FilterId: "path_id", Value: "90", Operand: "equals"},
						{FilterType: "value", FilterId: "row_id", Value: "32", Operand: "equals"},
					},
				},
			},
			validateJSON: func(t *testing.T, payload map[string]interface{}) {
				sf := payload["sceneFilter"].(map[string]interface{})
				meta := sf["metadataFilter"].(map[string]interface{})

				if meta["filterType"] != "and" {
					t.Fatalf("Expected root filterType to be 'and', got %v", meta["filterType"])
				}

				// assert JSON key matches your json struct tag: "childFilters"
				childFilters, ok := meta["childFilters"].([]interface{})
				if !ok {
					t.Fatalf("Expected childFilters block to be present in JSON")
				}
				if len(childFilters) != 2 {
					t.Errorf("Expected 2 nested child filters, got %d", len(childFilters))
				}
			},
		},
		{
			name: "Range Filter (Between layout compilation)",
			inputFilter: &SceneFilter{
				Metadata: &MetadataFilter{
					FilterType:  "between",
					FilterId:    "path_id",
					FirstValue:  "90",
					SecondValue: "95",
				},
			},
			validateJSON: func(t *testing.T, payload map[string]interface{}) {
				sf := payload["sceneFilter"].(map[string]interface{})
				meta := sf["metadataFilter"].(map[string]interface{})

				if meta["filterType"] != "between" {
					t.Errorf("Expected filterType 'between', got %v", meta["filterType"])
				}
				if meta["firstValue"] != "90" || meta["secondValue"] != "95" {
					t.Errorf("Expected range 90-95, got %v-%v", meta["firstValue"], meta["secondValue"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)

				var payload map[string]interface{}
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Fatalf("Invalid JSON payload sent over wire: %v", err)
				}

				tt.validateJSON(t, payload)

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"data":{"results":[],"totalHits":0}}`))
			}))
			defer server.Close()

			// pass testLogger here to satisfy the WithLogger option requirement
			client, err := NewClient("test", "test", 1, "./tmp", WithLogger(testLogger))
			if err != nil {
				t.Fatalf("Failed to initialize test client: %v", err)
			}
			client.baseURL = server.URL

			_, err = client.Request.SceneSearch(context.Background(), "landsat_ot_c2_l1", tt.inputFilter, 0)
			if err != nil {
				t.Fatalf("SceneSearch failed unexpectedly: %v", err)
			}
		})
	}
}
