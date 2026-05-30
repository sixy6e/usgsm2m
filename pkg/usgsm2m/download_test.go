package usgsm2m

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestFilterForPreviews(t *testing.T) {
	// input options and expected results
	inputOptions := []DownloadOption{
		{EntityId: "LC8123045", Id: "prod_1", ProductName: "Landsat Look Natural Color Image"},
		{EntityId: "LC8123045", Id: "prod_2", ProductName: "Level-1 OLI/TIRS Data Bundle"},
		{EntityId: "LC8123046", Id: "prod_3", ProductName: "Full Resolution Browse Selection"},
	}

	expected := []DownloadRequestItem{
		{EntityId: "LC8123045", ProductId: "prod_1"},
		{EntityId: "LC8123046", ProductId: "prod_3"},
	}

	result := FilterForPreviews(inputOptions)

	// assert
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("FilterForPreviews() = %v, want %v", result, expected)
	}
}

func TestFilterBySystem_Deduplication(t *testing.T) {
	inputOptions := []DownloadOption{
		{EntityId: "SCENE_A", Id: "id_1", DownloadSystem: "ls_zip", Available: true},
		// this duplicate scene should be skipped because seenEntities will track it!
		{EntityId: "SCENE_A", Id: "id_2", DownloadSystem: "ls_zip", Available: true},
		{EntityId: "SCENE_B", Id: "id_3", DownloadSystem: "ls_zip", Available: true},
	}

	result := FilterBySystem(inputOptions, "ls_zip")

	if len(result) != 2 {
		t.Errorf("Expected 2 items after deduplication, got %d", len(result))
	}
	if result[0].ProductId != "id_1" || result[1].ProductId != "id_3" {
		t.Errorf("Deduplication picked wrong product Ids: %v", result)
	}
}

func TestGetDownloadOptions_Success(t *testing.T) {
	// spin up the local mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"version":"1.0","errorCode":null,"errorMessage":"","data":[]}`))
	}))
	defer server.Close()

	// initialize using testing overrides
	client, err := NewClient(
		"test_user",
		"test_token",
		2,                               // maxWorkers
		t.TempDir(),                     // outputDir safely cleaned up by the test harness
		WithBaseURL(server.URL),         // force it to point to mock server
		WithHTTPClient(server.Client()), // force it to use the mock server's network channel
	)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	// run
	options, err := client.Request.GetDownloadOptions(context.Background(), "landsat_8", []string{"SCENE_1"})

	// assert
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if len(options) != 0 {
		t.Errorf("Expected empty slice, got %d items", len(options))
	}
}
