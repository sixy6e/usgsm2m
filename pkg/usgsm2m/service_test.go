package usgsm2m

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDoRequest_RateLimitBackoff(t *testing.T) {
	ctx := context.Background()
	var requestCount int32

	// create a mock HTTP server to simulate the USGS API behavior
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)

		w.Header().Set("Content-Type", "application/json")

		if count == 1 {
			// first attempt: simulate a USGS rate limit business error
			w.WriteHeader(http.StatusOK) // USGS frequently returns 200 OK for business errors
			errEnv := ResponseEnvelope[any]{
				Version:      "1.0",
				ErrorCode:    stringPtr("RATE_LIMIT"),
				ErrorMessage: "Concurrency limit exceeded. Please back off.",
			}
			_ = json.NewEncoder(w).Encode(errEnv)
			return
		}

		// second attempt: success payload
		w.WriteHeader(http.StatusOK)
		successEnv := ResponseEnvelope[string]{
			Version: "1.0",
			Data:    "success-payload",
		}
		_ = json.NewEncoder(w).Encode(successEnv)
	}))
	defer server.Close()

	// initialize a local Client targeting the mock server
	// use a discarded/null logger to keep the test terminal output clean
	discardLogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	client, err := NewClient("test-user", "test-token", 3, os.TempDir(), WithLogger(discardLogger))
	if err != nil {
		t.Fatalf("Failed to create test client: %v", err)
	}
	// override the base URL to redirect all transport calls to the mock server
	client.baseURL = server.URL

	// construct the service wrapper
	service := &RequestService{client: client}

	// execute the generic request targeting a string response shape
	var result string
	err = doRequest(ctx, service, "test-endpoint", nil, &result)

	// assertions
	if err != nil {
		t.Fatalf("Expected doRequest to eventually succeed, got error: %v", err)
	}

	finalCount := atomic.LoadInt32(&requestCount)
	if finalCount != 2 {
		t.Errorf("Expected exactly 2 requests (1 failure + 1 retry success), but got %d", finalCount)
	}

	if result != "success-payload" {
		t.Errorf("Expected result to be 'success-payload', got '%s'", result)
	}
}

// stringPtr is a helper to handle pointer strings inline
func stringPtr(s string) *string {
	return &s
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name            string
		inputError      error
		expectRetryable bool
	}{
		{
			name: "Network Timeout Error",
			// use Go's native context timeout error—this returns true for netErr.Timeout()!
			inputError:      context.DeadlineExceeded,
			expectRetryable: true,
		},
		{
			name:            "Server Side Gateway Error",
			inputError:      fmt.Errorf("unexpected http status: 502 Bad Gateway"),
			expectRetryable: true,
		},
		{
			name:            "Server Side Service Unavailable Error",
			inputError:      fmt.Errorf("unexpected http status: 503 Service Unavailable"),
			expectRetryable: true,
		},
		{
			name:            "Fatal Client Error (400 Bad Request)",
			inputError:      fmt.Errorf("unexpected http status: 400 Bad Request"),
			expectRetryable: false,
		},
		{
			name:            "Fatal Auth Error (401 Unauthorized)",
			inputError:      fmt.Errorf("unexpected http status: 401 Unauthorized"),
			expectRetryable: false,
		},
		{
			name:            "JSON Structural/Decoding Malformation",
			inputError:      fmt.Errorf("decode response: json: cannot unmarshal string into Go value"),
			expectRetryable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRetryable(tt.inputError)
			if got != tt.expectRetryable {
				t.Errorf("isRetryable() for '%s' = %v; want %v", tt.name, got, tt.expectRetryable)
			}
		})
	}
}

func TestDoRequest_ContextCancelledMidBackoff(t *testing.T) {
	// create a mock server that always fails with a retryable status code
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway) // 502 Triggering isRetryable() = true
	}))
	defer server.Close()

	// initialize a client redirecting to the mock server
	discardLogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	client, err := NewClient("test-user", "test-token", 3, os.TempDir(), WithLogger(discardLogger))
	if err != nil {
		t.Fatalf("Failed to create test client: %v", err)
	}
	client.baseURL = server.URL
	service := &RequestService{client: client}

	// create a cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	// concurrently trigger cancellation right after the first attempt hits the server
	// we use a small timer to give the engine time to process the first attempt and enter the backoff block
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	// track execution time to guarantee it doesn't wait out the regular backoff seconds
	startTime := time.Now()

	var result string
	err = doRequest(ctx, service, "test-endpoint", nil, &result)

	duration := time.Since(startTime)

	// assertions
	if err == nil {
		t.Fatal("Expected doRequest to return a context cancellation error, but got nil")
	}

	// ensure the error correctly wraps context.Canceled
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled error, got: %v", err)
	}

	// since retry backoffs start at 1s+ or 5s for penalty limits,
	// the test should break out almost instantly (well under 500ms).
	if duration > 500*time.Millisecond {
		t.Errorf("Execution took too long (%v); select block failed to short-circuit the backoff timer!", duration)
	}
}

func TestDoRequest_TokenInvalidationRecovery(t *testing.T) {
	ctx := context.Background()
	var requestCount int32

	// mock a dynamic server that forces an AUTH_FAILURE on first hit, then succeeds
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		// inspect if this is the explicit Login endpoint or the business endpoint
		if strings.Contains(r.URL.Path, "login") {
			loginEnv := ResponseEnvelope[string]{
				Version: "1.0",
				Data:    "fresh-refreshed-api-token",
			}
			_ = json.NewEncoder(w).Encode(loginEnv)
			return
		}

		// atomic increment must happen ONLY for the business endpoint requests,
		// ensuring the login requests don't advance our execution attempts count
		count := atomic.AddInt32(&requestCount, 1)

		if count == 1 {
			// first attempt to our business endpoint simulates a dead/expired token error
			errEnv := ResponseEnvelope[any]{
				Version:      "1.0",
				ErrorCode:    stringPtr("AUTH_FAILURE"),
				ErrorMessage: "Your active session token has expired or is invalid.",
			}
			_ = json.NewEncoder(w).Encode(errEnv)
			return
		}

		// second attempt (after transparent re-auth recovery) succeeds
		successEnv := ResponseEnvelope[string]{
			Version: "1.0",
			Data:    "recovered-payload-data",
		}
		_ = json.NewEncoder(w).Encode(successEnv)
	}))
	defer server.Close()

	// initialize the client redirection
	discardLogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	client, err := NewClient("test-user", "expired-initial-token", 2, os.TempDir(), WithLogger(discardLogger))
	if err != nil {
		t.Fatalf("Failed to create test client: %v", err)
	}
	client.baseURL = server.URL
	service := &RequestService{client: client}

	// execute the request
	var result string
	err = doRequest(ctx, service, "dataset-endpoint", nil, &result)

	// assertions
	if err != nil {
		t.Fatalf("Expected client to transparently recover from auth failure, got error: %v", err)
	}

	if result != "recovered-payload-data" {
		t.Errorf("Expected result to be 'recovered-payload-data', got '%s'", result)
	}

	// confirm that the client instance's internal API key was updated with the fresh token
	client.mu.Lock()
	finalKey := client.apiKey
	client.mu.Unlock()

	if finalKey != "fresh-refreshed-api-token" {
		t.Errorf("Expected internal client apiKey to be updated to the fresh token, got: %s", finalKey)
	}
}
