package usgsm2m

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	username   string
	token      string
	mu         sync.Mutex
	logger     *slog.Logger

	// Services
	Request    *RequestService
	Downloader *DownloadManager
}

// Option defines the functional option type for Client creation
type Option func(*Client)

// WithTimeout allows users to override the default 60s timeout
func WithTimeout(t time.Duration) Option {
	return func(c *Client) {
		c.httpClient.Timeout = t
	}
}

// WithCustomEndpoint is useful for testing or future API versions
func WithCustomEndpoint(url string) Option {
	return func(c *Client) {
		c.baseURL = url
	}
}

func WithLogger(l *slog.Logger) Option {
	return func(c *Client) {
		if l != nil {
			c.logger = l
		}
	}
}

// NewClient initializes the USGS M2M Client.
func NewClient(username, token string, maxWorkers int, outputDir string, opts ...Option) (*Client, error) {
	c := &Client{
		httpClient: &http.Client{
			Timeout: 0,
			Transport: &http.Transport{
				DialContext: (&net.Dialer{
					Timeout:   30 * time.Second, // drop connection if handshake fails
					KeepAlive: 30 * time.Second,
				}).DialContext,
				TLSHandshakeTimeout:   15 * time.Second,
				ResponseHeaderTimeout: 45 * time.Second, // drop connection if USGS chokes on headers
			},
		},
		baseURL:  "https://m2m.cr.usgs.gov/api/api/json/stable/",
		username: username,
		token:    token,
		logger:   slog.Default(),
	}

	for _, opt := range opts {
		opt(c)
	}

	dm, err := NewDownloadManager(c, maxWorkers, outputDir)
	if err != nil {
		return nil, err
	}

	c.Downloader = dm

	c.Request = &RequestService{client: c}
	return c, nil
}

// Login performs the initial authentication to get the API Key.
func (c *Client) Login(ctx context.Context) error {
	req := map[string]string{
		"username": c.username,
		"token":    c.token,
	}

	var resp struct {
		Data string `json:"data"` // The API Key is usually in the "data" field
	}

	// use login-token instead of the legacy login
	err := c.Request.doRequest(ctx, "login-token", req, &resp)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	c.mu.Lock()
	c.apiKey = resp.Data
	c.mu.Unlock()
	c.logger.Info("Successfully authenticated with M2M", "session_active", true)
	return nil
}

func (c *Client) Logout(ctx context.Context) error {
	// read the key under lock, then release immediately
	c.mu.Lock()
	key := c.apiKey
	c.mu.Unlock()

	if key == "" {
		return nil
	}

	var resp struct {
		Data bool `json:"data"`
	}

	// perform the network call WITHOUT holding the lock
	// c.doRequest will manage its own locking for headers
	err := c.Request.doRequest(ctx, "logout", nil, &resp)

	// clear the key under lock
	c.mu.Lock()
	c.apiKey = ""
	c.mu.Unlock()

	c.logger.Info("Session closed and API key cleared")
	return err
}

func (c *Client) doRequest(ctx context.Context, endpoint string, payload interface{}, result interface{}) error {
	// build the full URL
	fullURL := strings.TrimSuffix(c.baseURL, "/") + "/" + endpoint

	// marshal the request body
	var bodyReader io.Reader
	if payload != nil {
		jsonData, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewBuffer(jsonData)
	}

	// create the HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", fullURL, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	// set specific headers
	c.mu.Lock()
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("X-Auth-Token", c.apiKey)
	}
	c.mu.Unlock()

	// execute
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http call failed: %w", err)
	}
	defer resp.Body.Close()

	// // ******************* temp block **********************************
	// // Read all the raw bytes out of the network response body stream
	// bodyBytes, err := io.ReadAll(resp.Body)
	// if err != nil {
	// 	return fmt.Errorf("failed to read response body for debugging: %w", err)
	// }

	// // Format the JSON nicely so it's readable (indent with spaces)
	// var prettyJSON bytes.Buffer
	// if err := json.Indent(&prettyJSON, bodyBytes, "", "    "); err == nil {
	// 	fmt.Println("--- DEBUG RAW API RESPONSE ---")
	// 	fmt.Println(prettyJSON.String())
	// 	fmt.Println("------------------------------")
	// } else {
	// 	// Fallback to raw string if it's not valid JSON
	// 	fmt.Printf("--- DEBUG RAW STRING ---\n%s\n------------------------\n", string(bodyBytes))
	// }

	// // CRITICAL: Put the bytes BACK into a stream so the decoder can still read it!
	// resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	// decode JSON response
	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	// USGS API-Level Error Handling & Auto-Refresh
	if r, ok := result.(Response); ok {
		base := r.GetBase()
		if base == nil {
			return fmt.Errorf("internal error: base response is nil")
		}

		if apiErr := base.HasError(); apiErr != nil {
			// Check for expired token to trigger auto-login
			// Replace "AUTH_ERROR" with whatever code USGS actually returns for expiry
			if base.ErrorCode != nil && *base.ErrorCode == "AUTH_ERROR" && endpoint != "login-token" {
				c.logger.Warn("USGS Token expired, attempting auto-refresh")

				// attempt to re-login using the context
				if err := c.Login(ctx); err == nil {
					// recursive call with the NEW apiKey
					return c.doRequest(ctx, endpoint, payload, result)
				}
			}
			return apiErr
		}
	}

	return nil
}

func (c *Client) Cleanup(ctx context.Context, sceneListName string) {
	c.logger.Info("Starting system cleanup", "batch_label", sceneListName)

	// remove the Scene List from USGS
	// this prevents the "list already exists" error on a users next run
	if sceneListName != "" {
		err := c.Request.RemoveSceneList(ctx, sceneListName)
		if err != nil {
			c.logger.Warn("Failed to remove USGS scene list", "error", err)
		} else {
			c.logger.Info("USGS scene list removed successfully")
		}
	}

	// finally; logout
	if err := c.Logout(ctx); err != nil {
		c.logger.Warn("USGS logout failed", "error", err)
	} else {
		c.logger.Info("USGS session closed")
	}
}

// FetchDatasetMetadata uses the "dataset-metadata" endpoint to retrieve all metadata fields for a given dataset
func (c *Client) FetchDatasetMetadata(ctx context.Context, datasetName string) (map[string][]M2MFieldID, error) {
	req := DatasetMetadataRequest{
		DatasetName: datasetName,
	}

	var resp DatasetMetadataResponse

	err := c.doRequest(ctx, "dataset-metadata", req, &resp)
	if err != nil {
		return nil, err
	}

	return resp.Data, nil
}

// FetchDatasetFilters retrieves searchable parameters and valid option mappings for scene queries
func (c *Client) FetchDatasetFilters(ctx context.Context, datasetName string) ([]DatasetFilterField, error) {
	// payload for the endpoint
	reqPayload := map[string]string{
		"datasetName": datasetName,
	}

	var respEnvelope DatasetFiltersResponse

	// fire the network call
	err := c.doRequest(ctx, "dataset-filters", reqPayload, &respEnvelope)
	if err != nil {
		return nil, fmt.Errorf("network execution failed for dataset-filters: %w", err)
	}

	// handle explicit error responses sent down by the USGS API
	if respEnvelope.ErrorMessage != "" || respEnvelope.ErrorCode != "" {
		return nil, fmt.Errorf("USGS API error (%s): %s", respEnvelope.ErrorCode, respEnvelope.ErrorMessage)
	}

	return respEnvelope.Data, nil
}
