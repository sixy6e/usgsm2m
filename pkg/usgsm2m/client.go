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

// GetDownloadOptions returns the available products (bundles, images, etc.) for a list of scenes.
func (c *Client) GetDownloadOptions(ctx context.Context, dataset string, entityIds []string) ([]DownloadOption, error) {
	req := map[string]interface{}{
		"datasetName": dataset,
		"entityIds":   entityIds,
	}

	var resp struct {
		BaseResponse
		Data []DownloadOption `json:"data"`
	}

	err := c.doRequest(ctx, "download-options", req, &resp)
	if err != nil {
		return nil, err
	}

	return resp.Data, nil
}

// RequestDownload submits a list of Download IDs to the USGS for processing.
func (c *Client) RequestDownload(ctx context.Context, downloadIds []string, label string) (*DownloadRequestResponse, error) {
	// wrap the IDs into the format USGS expects
	downloads := make([]map[string]string, len(downloadIds))
	for i, id := range downloadIds {
		downloads[i] = map[string]string{"downloadId": id}
	}

	req := map[string]interface{}{
		"downloads": downloads,
		"label":     label,
	}

	var resp DownloadRequestResponse
	err := c.doRequest(ctx, "download-request", req, &resp)
	if err != nil {
		return nil, err
	}

	return &resp, nil
}

// SmartBatchRequest finds the 'Bundle' option for each EntityID and requests the download.
func (c *Client) SmartBatchRequest(ctx context.Context, dataset string, entityIds []string, label string) error {
	// we'll ask for all options
	opts, err := c.GetDownloadOptions(ctx, dataset, entityIds)
	if err != nil {
		return err
	}

	// TODO check and confirm whether "bundle" can work for other datasets eg modis viirs
	// filter for "Bundles" (Level-1 GeoTIFF Product Bundle)
	// could build other filters
	var downloadIds []string
	for _, opt := range opts {
		if opt.Available && strings.Contains(strings.ToLower(opt.ProductName), "bundle") {
			downloadIds = append(downloadIds, opt.Id)
		}
	}

	if len(downloadIds) == 0 {
		return fmt.Errorf("no download bundles found for the provided scenes")
	}

	// submit the download request
	_, err = c.RequestDownload(ctx, downloadIds, label)
	return err
}

func (c *Client) SceneSearch(ctx context.Context, req SceneSearchRequest) (*SceneSearchResponse, error) {
	var resp SceneSearchResponse

	// Default MaxResults to 100 if the user didn't specify,
	// matching the struct's comment expectation.
	if req.MaxResults == 0 {
		req.MaxResults = 100
	}

	// doRequest handles the JSON marshaling and the API Key header
	err := c.doRequest(ctx, "scene-search", req, &resp)
	if err != nil {
		return nil, fmt.Errorf("scene search failed: %w", err)
	}

	return &resp, nil
}

func (c *Client) SceneSearchAll(ctx context.Context, req SceneSearchRequest) ([]Scene, error) {
	var allScenes []Scene

	// start from the first record
	req.StartingNumber = 1
	if req.MaxResults == 0 {
		req.MaxResults = 100 // USGS maximum per request is usually 100
	}

	for {
		resp, err := c.SceneSearch(ctx, req)
		if err != nil {
			return nil, err
		}

		// collect the results from this page
		allScenes = append(allScenes, resp.Data.Results...)

		// check if we've reached the end
		// if NextRecord is 0 or less, then we're done
		if resp.Data.NextRecord <= 0 {
			break
		}

		// update the starting point for the next loop
		req.StartingNumber = resp.Data.NextRecord

		// fmt.Printf("[Search] Collected %d / %d total hits...\n", len(allScenes), resp.Data.TotalHits)
		c.logger.Info("Scene Search", "total_scenes", len(allScenes), "total_hits", resp.Data.TotalHits)
	}

	return allScenes, nil
}

func (c *Client) GetAllDownloadOptions(ctx context.Context, dataset string, entityIds []string) ([]DownloadOption, error) {
	var allOptions []DownloadOption
	c.logger.Info("Starting download-options metadata retrieval", "total_scenes", len(entityIds))

	// split into chunks of 100
	batches := BatchSlice(entityIds, 100)

	for _, batch := range batches {
		c.logger.Debug("Requesting metadata options", "batch_size", len(batch))

		options, err := c.GetDownloadOptions(ctx, dataset, batch)
		if err != nil {
			return nil, err
		}

		allOptions = append(allOptions, options...)
	}

	return allOptions, nil
}

func (c *Client) DownloadRequest(ctx context.Context, items []DownloadRequestItem) (*DownloadRequestResponse, error) {
	// combined response to hold all batches
	finalResp := &DownloadRequestResponse{}

	// batch the items (USGS limit is 100)
	batches := BatchSlice(items, 100)

	for _, batch := range batches {
		payload := DownloadRequestPayload{
			Downloads: batch,
			Label:     "Go-M2M-Downloader",
		}

		var batchResp DownloadRequestResponse
		err := c.doRequest(ctx, "download-request", payload, &batchResp)
		if err != nil {
			return nil, fmt.Errorf("batch download request failed: %w", err)
		}

		// merge this batch's results into our final response
		finalResp.Data.Available = append(finalResp.Data.Available, batchResp.Data.Available...)
		finalResp.Data.Preparing = append(finalResp.Data.Preparing, batchResp.Data.Preparing...)
		finalResp.Data.Failed = append(finalResp.Data.Failed, batchResp.Data.Failed...)
	}

	return finalResp, nil
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
