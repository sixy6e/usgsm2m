package usgsm2m

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetFilenameFromHeaders(t *testing.T) {
	tests := []struct {
		name        string
		disposition string
		defaultName string
		expected    string
	}{
		{
			name:        "Standard filename parameter",
			disposition: `attachment; filename="landsat_scene_123.tar.gz"`,
			defaultName: "fallback.tar.gz",
			expected:    "landsat_scene_123.tar.gz",
		},
		{
			name:        "Extended UTF-8 encoding (filename*)",
			disposition: `attachment; filename*=UTF-8''landsat_scene_456.tar.gz`,
			defaultName: "fallback.tar.gz",
			expected:    "landsat_scene_456.tar.gz",
		},
		{
			name:        "Empty or missing header entirely",
			disposition: "",
			defaultName: "fallback.tar.gz",
			expected:    "fallback.tar.gz",
		},
		{
			name:        "Malformed or broken disposition syntax",
			disposition: `attachment; filename=missing-quotes-but-broken;=`,
			defaultName: "fallback.tar.gz",
			expected:    "fallback.tar.gz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{Header: make(http.Header)}
			if tt.disposition != "" {
				resp.Header.Set("Content-Disposition", tt.disposition)
			}
			got := GetFilenameFromHeaders(resp, tt.defaultName)
			if got != tt.expected {
				t.Errorf("GetFilenameFromHeaders() = %q; want %q", got, tt.expected)
			}
		})
	}
}

func TestBatchSlice(t *testing.T) {
	items := []int{1, 2, 3, 4, 5, 6, 7}

	t.Run("Standard even breaking batch size", func(t *testing.T) {
		batches := BatchSlice(items, 3)
		if len(batches) != 3 { // should be [[1,2,3], [4,5,6], [7]]
			t.Fatalf("Expected 3 batches, got %d", len(batches))
		}
		if len(batches[2]) != 1 || batches[2][0] != 7 {
			t.Errorf("Last trailing partial batch built incorrectly: %v", batches[2])
		}
	})

	t.Run("Batch size larger than input length", func(t *testing.T) {
		batches := BatchSlice(items, 20)
		if len(batches) != 1 || len(batches[0]) != 7 {
			t.Errorf("Expected single total snapshot batch, got: %v", batches)
		}
	})

	t.Run("Empty slice input", func(t *testing.T) {
		var empty []string
		batches := BatchSlice(empty, 5)
		if len(batches) != 0 {
			t.Errorf("Expected empty slice return context, got %d batches", len(batches))
		}
	})
}

func TestGenerateBatchId_Format(t *testing.T) {
	id1 := GenerateBatchId()
	id2 := GenerateBatchId()

	if !strings.HasPrefix(id1, "batch-") {
		t.Errorf("Batch ID structure wrong: %s", id1)
	}
	if len(id1) != 12 { // "batch-" (6) + random charset (6) = 12 characters
		t.Errorf("Expected length 12, got %d", len(id1))
	}
	if id1 == id2 {
		t.Errorf("Identifier generated a collision: %s == %s", id1, id2)
	}
}

func TestDatasetPointersAndFallbacks(t *testing.T) {
	aliasText := "LC08_L1TP"
	temporalText := "2013-01-01 to Present"

	mockDatasets := []Dataset{
		{
			DatasetId:        "landsat_8",
			DatasetAlias:     &aliasText,
			TemporalCoverage: &temporalText,
		},
		{
			DatasetId:        "empty_landsat",
			DatasetAlias:     nil, // forces FromPtrOr fallback path
			TemporalCoverage: nil, // forces FromPtrOr fallback path
		},
	}

	t.Run("DatasetNames extracts matching elements", func(t *testing.T) {
		names := DatasetNames(mockDatasets)
		if len(names) != 2 || names[0] != "LC08_L1TP" || names[1] != "" {
			t.Errorf("DatasetNames extraction mismatch: %v", names)
		}
	})

	t.Run("DatasetTemporalCoverage extracts defaults", func(t *testing.T) {
		coverage := DatasetTemporalCoverage(mockDatasets)
		if len(coverage) != 2 || coverage[0] != "2013-01-01 to Present" || coverage[1] != "Unknown" {
			t.Errorf("DatasetTemporalCoverage fallback failed: %v", coverage)
		}
	})
}

func TestParseGeoJSONFile_Matrix(t *testing.T) {
	tmpDir := t.TempDir()

	// setup raw byte test mock structures matching the 3 schema variants
	fcJSON := `{"type":"FeatureCollection","features":[{"type":"Feature","geometry":{"type":"Point","coordinates":[100.0,0.0]}}]}`
	fJSON := `{"type":"Feature","geometry":{"type":"LineString","coordinates":[[100.0,0.0],[101.0,1.0]]}}`
	gJSON := `{"type":"Polygon","coordinates":[[[100.0,0.0],[101.0,0.0],[101.0,1.0],[100.0,0.0]]].}` // Typo deliberately added for failure checks
	gValidJSON := `{"type":"Polygon","coordinates":[[[100.0,0.0],[101.0,0.0],[101.0,1.0],[100.0,0.0]]]}`

	// write helper closure
	writeTmp := func(name, content string) string {
		p := filepath.Join(tmpDir, name)
		_ = os.WriteFile(p, []byte(content), 0644)
		return p
	}

	t.Run("Parses Valid FeatureCollection", func(t *testing.T) {
		path := writeTmp("fc.geojson", fcJSON)
		geom, err := ParseGeoJSONFile(path)
		if err != nil || geom.Type != "Point" {
			t.Errorf("Failed to parse FeatureCollection: %v", err)
		}
	})

	t.Run("Parses Standalone Feature Block", func(t *testing.T) {
		path := writeTmp("f.geojson", fJSON)
		geom, err := ParseGeoJSONFile(path)
		if err != nil || geom.Type != "LineString" {
			t.Errorf("Failed to parse Feature payload: %v", err)
		}
	})

	t.Run("Parses Naked Geometry Shape", func(t *testing.T) {
		path := writeTmp("g.geojson", gValidJSON)
		geom, err := ParseGeoJSONFile(path)
		if err != nil || geom.Type != "Polygon" {
			t.Errorf("Failed to parse naked geometry: %v", err)
		}
	})

	t.Run("Fails cleanly on missing filesystem path", func(t *testing.T) {
		_, err := ParseGeoJSONFile(filepath.Join(tmpDir, "ghost.geojson"))
		if err == nil {
			t.Fatal("Expected missing file error, got nil")
		}
	})

	t.Run("Fails cleanly on corrupted schema structural data", func(t *testing.T) {
		path := writeTmp("bad.geojson", gJSON)
		_, err := ParseGeoJSONFile(path)
		if err == nil {
			t.Fatal("Expected syntax schema processing failure, got nil")
		}
	})
}

func TestFieldResolver_Resolve(t *testing.T) {
	// setup a mock server delivering a valid dataset profile envelope
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		// mock output structure representing dynamic map[string][]M2MFieldID response profile payload
		mockPayload := map[string][]M2MFieldID{
			"export": {
				{ID: "5e81f1502c7f8da4", FieldName: "WRS Path"},
				{ID: "6b22c8331a9f0da1", FieldName: "WRS Row"},
			},
		}

		env := ResponseEnvelope[map[string][]M2MFieldID]{
			Version: "1.0",
			Data:    mockPayload,
		}
		_ = json.NewEncoder(w).Encode(env)
	}))
	defer server.Close()

	// setup the core mock client state redirects
	client, err := NewClient("test-user", "test-token", 1, t.TempDir())
	if err != nil {
		t.Fatalf("Failed to initialise client framework: %v", err)
	}
	client.baseURL = server.URL
	resolver := NewFieldResolver(client)

	ctx := context.Background()

	t.Run("Cache Miss loads dynamically from remote endpoint", func(t *testing.T) {
		id, err := resolver.Resolve(ctx, "landsat_8", "WRS Path")
		if err != nil {
			t.Fatalf("Expected successful resolution path, got: %v", err)
		}
		if id != "5e81f1502c7f8da4" {
			t.Errorf("Expected id '5e81f1502c7f8da4', got %q", id)
		}
	})

	t.Run("Cache Hit returns token instantly without needing the network server", func(t *testing.T) {
		// break the base URL entirely. if it attempts to make a network round-trip, it will crash,
		// verifying the resolution is safely answered out of local cache memory
		client.baseURL = "http://127.0.0.1:0"

		id, err := resolver.Resolve(ctx, "landsat_8", "WRS Row")
		if err != nil {
			t.Fatalf("Expected cached response value mapping, got error: %v", err)
		}
		if id != "6b22c8331a9f0da1" {
			t.Errorf("Expected id '6b22c8331a9f0da1', got %q", id)
		}
	})

	t.Run("Invalid field request returns error formatting", func(t *testing.T) {
		_, err := resolver.Resolve(ctx, "landsat_8", "NonExistentField")
		if err == nil {
			t.Fatal("Expected validation resolution failure error, got nil")
		}
	})
}
