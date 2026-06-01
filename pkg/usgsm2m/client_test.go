package usgsm2m

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
)

func TestClient_ConcurrentAccessSafety(t *testing.T) {
	ctx := context.Background()

	// create a mock server that returns a simple valid success payload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		successEnv := ResponseEnvelope[string]{
			Version: "1.0",
			Data:    "concurrent-safe",
		}
		_ = json.NewEncoder(w).Encode(successEnv)
	}))
	defer server.Close()

	// initialize the client, targeting the mock server
	discardLogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	client, err := NewClient("test-user", "test-token", 1, os.TempDir(), WithLogger(discardLogger))
	if err != nil {
		t.Fatalf("Failed to create test client: %v", err)
	}
	client.baseURL = server.URL
	service := &RequestService{client: client}

	// setup concurrency controls
	const goroutineCount = 20
	var wg sync.WaitGroup

	// launch multiple goroutines reading/writing to the client state concurrently
	for i := 0; i < goroutineCount; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// concurrently simulate hitting an endpoint (reads API key state)
			var result string
			if err := doRequest(ctx, service, "concurrent-endpoint", nil, &result); err != nil {
				t.Errorf("Worker %d encountered error: %v", id, err)
			}

			// simulating manual token updates or login triggers concurrently (writes state)
			client.mu.Lock()
			client.apiKey = "dynamically-swapped-token"
			client.mu.Unlock()
		}(i)
	}

	// wait for all execution branches to terminate
	wg.Wait()
}

func TestLogout_HandlesEmptyKey(t *testing.T) {
	// initialize a client with an explicitly EMPTY API token string
	discardLogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	client, err := NewClient("test-user", "", 3, os.TempDir(), WithLogger(discardLogger))
	if err != nil {
		t.Fatalf("Failed to create test client: %v", err)
	}

	// we intentionally DO NOT spin up a httptest.NewServer here.
	// If the code tries to make an HTTP network request, it will panic or crash
	// because the base URL isn't configured, proving that it failed to short-circuit!
	ctx := context.Background()
	err = client.Logout(ctx)

	// assertion: it should return nil instantly without making network calls
	if err != nil {
		t.Errorf("Expected Logout() to short-circuit and return nil on an empty API key, but got: %v", err)
	}
}
