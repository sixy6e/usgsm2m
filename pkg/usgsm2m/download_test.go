package usgsm2m

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
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

func TestGetDownloadOptions_USGSError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK) // Note: USGS can return HTTP 200 even for error payloads

		// simulated M2M Error structure
		mockErrorJSON := `{
			"version": "1.0",
			"errorCode": "INVALID_DATASET",
			"errorMessage": "The requested dataset name 'landsat_invalid' was not recognized.",
			"data": null
		}`
		w.Write([]byte(mockErrorJSON))
	}))
	defer server.Close()

	client, err := NewClient(
		"test_user", "test_token", 2, t.TempDir(),
		WithBaseURL(server.URL),
		WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	// this should return an error because doRequest internally invokes envelope.Error()
	_, err = client.Request.GetDownloadOptions(context.Background(), "landsat_invalid", []string{"SCENE_1"})
	if err == nil {
		t.Fatal("Expected an error from an invalid dataset payload, but got nil")
	}

	// verify our custom error string layout is being built properly
	expectedSubstr := "USGS Error (INVALID_DATASET)"
	if !strings.Contains(err.Error(), expectedSubstr) {
		t.Errorf("Expected error message to contain %q, but got: %v", expectedSubstr, err)
	}
}

func TestGetDownloadOptions_ServerChoked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway) // simulate a 502 error
		w.Write([]byte("<html><body>502 Bad Gateway</body></html>"))
	}))
	defer server.Close()

	client, err := NewClient(
		"test_user", "test_token", 2, t.TempDir(),
		WithBaseURL(server.URL),
		WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	_, err = client.Request.GetDownloadOptions(context.Background(), "landsat_8", []string{"SCENE_1"})
	if err == nil {
		t.Fatal("Expected an error from an HTTP 502, but execution succeeded")
	}

	if !strings.Contains(err.Error(), "unexpected http status") {
		t.Errorf("Expected error to mention HTTP status, got: %v", err)
	}
}

func TestGetDownloadURLs_PollingLoopSuccess(t *testing.T) {
	// keep track of how many times the client hits the mock server endpoints
	var retrieveCount int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		// extract the M2M endpoint path
		// e.g., if path is /download-request or /download-retrieve
		switch {
		case strings.HasSuffix(r.URL.Path, "download-request"):
			// firstly, tell the loop that our scene is stuck in cold/staging storage
			mockJSON := `{
				"version": "1.0",
				"errorCode": null,
				"data": {
					"availableDownloads": [],
					"preparingDownloads": [{"entityId": "LC8123045", "url": ""}]
				}
			}`
			w.Write([]byte(mockJSON))

		case strings.HasSuffix(r.URL.Path, "download-retrieve"):
			retrieveCount++

			if retrieveCount == 1 {
				// round 1 of polling: USGS preparing data
				mockJSON := `{
					"version": "1.0",
					"errorCode": null,
					"data": {
						"availableDownloads": [],
						"preparingDownloads": [{"entityId": "LC8123045", "url": ""}]
					}
				}`
				w.Write([]byte(mockJSON))
			} else {
				// round 2 of polling: data ready
				mockJSON := `{
					"version": "1.0",
					"errorCode": null,
					"data": {
						"availableDownloads": [{"entityId": "LC8123045", "url": "https://storage.usgs.gov/landsat/LC8123045.tar.gz"}],
						"preparingDownloads": []
					}
				}`
				w.Write([]byte(mockJSON))
			}

		default:
			t.Errorf("Unexpected endpoint hit during polling test: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewClient(
		"test_user", "test_token", 2, t.TempDir(),
		WithBaseURL(server.URL),
		WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatalf("NewClient initialization failed: %v", err)
	}

	// so tests don't hang around waiting
	client.Request.pollInterval = 1 * time.Millisecond

	// short-lived background context
	ctx := context.Background()

	items := []DownloadRequestItem{
		{EntityId: "LC8123045", ProductId: "bundle_prod_id"},
	}

	// execute the orchestrated method execution loop
	links, err := client.Request.GetDownloadURLs(ctx, items)
	if err != nil {
		t.Fatalf("GetDownloadURLs execution failed: %v", err)
	}

	// --- ASSERTI0NS ---

	// verify that we polled exactly twice before breaking out
	if retrieveCount != 2 {
		t.Errorf("Expected polling loop to query retrieve endpoint exactly 2 times, but it polled %d times", retrieveCount)
	}

	// verify that our map contains the hot URL asset extracted at the finish line
	expectedURL := "https://storage.usgs.gov/landsat/LC8123045.tar.gz"
	if url, exists := links["LC8123045"]; !exists || url != expectedURL {
		t.Errorf("Failed to harvest download link. Got mapping: %v, Expected URL: %s", links, expectedURL)
	}
}

func TestGetDownloadURLs_ContextTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// force the server thread to pause slightly.
		// this should ensure context.WithTimeout(5ms) triggers a deadline breach
		time.Sleep(10 * time.Millisecond)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// constantly return "preparing" so the loop wants to run forever
		w.Write([]byte(`{"version":"1.0","data":{"availableDownloads":[],"preparingDownloads":[{"downloadId": 999123, "entityId":"LC8123045"}]}}`))
	}))
	defer server.Close()

	client, err := NewClient("user", "token", 2, t.TempDir(), WithBaseURL(server.URL), WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	// speed up the ticker so it loops quickly until the context expires
	client.Request.pollInterval = 1 * time.Millisecond

	// create a context that expires almost immediately (e.g., 5 milliseconds)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	items := []DownloadRequestItem{{EntityId: "LC8123045", ProductId: "prod_1"}}

	_, err = client.Request.GetDownloadURLs(ctx, items)

	// assert that it didn't hang and returned a context deadline error
	if err == nil {
		t.Fatal("Expected an error due to context timeout, but got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Errorf("Expected context deadline error, got: %v", err)
	}
}

func TestNewDownloadManager_Validation(t *testing.T) {
	tmpDir := t.TempDir()
	client, _ := NewClient("test", "token", 1, tmpDir)

	t.Run("Creates output directory if missing", func(t *testing.T) {
		targetPath := filepath.Join(tmpDir, "nested_output_dir")
		dm, err := NewDownloadManager(client, 2, targetPath)
		if err != nil {
			t.Fatalf("Expected successful DownloadManager initialization, got: %v", err)
		}
		if !FileExists(targetPath) {
			t.Errorf("DownloadManager failed to create target output directory path")
		}
		dm.Wait()
	})

	t.Run("Fails cleanly if directory permissions are completely unwritable", func(t *testing.T) {
		// attempting to target a protected system root path should trigger a write error
		_, err := NewDownloadManager(client, 2, "/sys/kernel/security/illegal_non_existent_path")
		if err == nil {
			t.Fatal("Expected error setup validation failure on unwritable path, got nil")
		}
	})
}

func TestDownloadManager_SuccessfulStreamAndRetryLogic(t *testing.T) {
	tmpDir := t.TempDir()

	// spin up a mock download data stream server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-gzip")
		w.Header().Set("Content-Disposition", `attachment; filename="landsat_scene_mock.tar.gz"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("mock-compressed-binary-payload-data-stream"))
	}))
	defer server.Close()

	client, _ := NewClient("test", "token", 1, tmpDir)
	dm, err := NewDownloadManager(client, 2, tmpDir)
	if err != nil {
		t.Fatalf("Failed to create DownloadManager: %v", err)
	}

	ctx := context.Background()

	t.Run("Streams file completely and performs safe atomic rename", func(t *testing.T) {
		job := DownloadJob{
			EntityId:    "LC08_L1TP_030032_20260601",
			ProductName: "Bundle",
			URL:         server.URL + "/stream-target",
		}

		err := dm.DownloadWithRetry(ctx, job)
		if err != nil {
			t.Fatalf("Expected clean successful download execution, got: %v", err)
		}

		// verify the folder structure matches: outputDir/ProductName/realFilename
		expectedFile := filepath.Join(tmpDir, "Bundle", "landsat_scene_mock.tar.gz")
		if !FileExists(expectedFile) {
			t.Errorf("Expected downloaded file to exist at: %s", expectedFile)
		}

		// verify that temp trailing file fragments are cleanly removed out
		if FileExists(expectedFile + ".tmp") {
			t.Errorf("Stale dangling .tmp download fragment was not cleared up")
		}
	})

	t.Run("Short-circuits instantly if target file already sits on disk", func(t *testing.T) {
		job := DownloadJob{
			EntityId:    "LC08_L1TP_030032_20260601",
			ProductName: "Bundle",
			URL:         server.URL + "/stream-target",
		}

		// call it again. this should hit the logic: d.client.logger.Info("File already exists, skipping")
		err := dm.DownloadWithRetry(ctx, job)
		if err != nil {
			t.Errorf("Expected nil error bypass return when file exists on local storage, got: %v", err)
		}
	})

	t.Run("Breaks out instantly without wasting retries on 404 permanent failure gates", func(t *testing.T) {
		deadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound) // 404
		}))
		defer deadServer.Close()

		job := DownloadJob{
			EntityId:    "LC08_Missing_Scene",
			ProductName: "Bundle",
			URL:         deadServer.URL,
		}

		startTime := time.Now()
		err := dm.DownloadWithRetry(ctx, job)
		duration := time.Since(startTime)

		if err == nil {
			t.Fatal("Expected download loop framework failure mapping, got nil")
		}

		// if it tried to back off and retry 3 times, the duration would exceed 2-4 seconds.
		// a non-retryable 404 should drop out of the loop under 500ms
		if duration > 1*time.Second {
			t.Errorf("Download engine wasted time retrying a permanent 404 error code gate: took %v", duration)
		}
	})

	dm.Wait()
}

func TestDownloadManager_ContextCancellationMitigation(t *testing.T) {
	tmpDir := t.TempDir()
	client, _ := NewClient("test", "token", 1, tmpDir)
	dm, _ := NewDownloadManager(client, 1, tmpDir)

	// create a long-standing mock stream that hangs to test mid-stream cancellation
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		time.Sleep(2 * time.Second) // force a delay block
	}))
	defer server.Close()

	// spin up a cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	job := DownloadJob{
		EntityId:    "LC08_Cancel_Me_Mid_Flight",
		ProductName: "Bundle",
		URL:         server.URL,
	}

	// trigger cancellation immediately after queueing up our worker payload
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := dm.DownloadWithRetry(ctx, job)
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("Expected error to explicitly trap context cancellation, got: %v", err)
	}

	// double check filesystem hygiene: verify the partial .tmp file was stripped from disk
	expectedTmpFile := filepath.Join(tmpDir, "Bundle", "LC08_Cancel_Me_Mid_Flight.tar.gz.tmp")
	if FileExists(expectedTmpFile) {
		t.Errorf("Filesystem failure: partial download scratch file %s was left behind after context cancellation", expectedTmpFile)
	}

	dm.Wait()
}

func TestDownloadOptionFlattenerAndFilters(t *testing.T) {
	mockOptions := []DownloadOption{
		{
			Id:             "prod_1",
			EntityId:       "scene_A",
			ProductName:    "Landsat Level-1 Product Bundle",
			DownloadSystem: "dds",
			Available:      true,
			ProductCode:    "L1B",
		},
		{
			Id:             "recursive_parent",
			EntityId:       "scene_B",
			ProductName:    "Parent Structure",
			DownloadSystem: "dds",
			Available:      true,
			ProductCode:    "",
			SecondaryDownloads: []DownloadOption{
				{
					Id:             "leaf_child",
					EntityId:       "scene_B",
					ProductName:    "Metadata XML Component File",
					DownloadSystem: "ls_zip",
					Available:      true,
					ProductCode:    "XML",
				},
			},
		},
	}

	t.Run("flattenOptions properly extracts nested leaf components", func(t *testing.T) {
		flat := flattenOptions(mockOptions[1]) // target the recursive parent element
		if len(flat) != 1 || flat[0].Id != "leaf_child" {
			t.Errorf("Expected 1 flattened leaf child node, got: %d", len(flat))
		}
	})
}
