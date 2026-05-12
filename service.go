package usgsm2m

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

type RequestService struct {
	client *Client
}

// Cleanup removes the temporary scene list from the USGS server.
func (s *RequestService) Cleanup(ctx context.Context, listId string) {
	if listId == "" {
		return
	}

	req := map[string]string{
		"listId": listId,
	}

	var resp BaseResponse

	// use a background-style log here because the job is technically "done"
	err := s.doRequest(ctx, "scene-list-remove", req, &resp)
	if err != nil {
		s.client.logger.Warn("Failed to clean up remote scene list", "listId", listId, "err", err)
	} else {
		s.client.logger.Info("Successfully removed remote scene list", "listId", listId)
	}
}

func (s *RequestService) doRequest(ctx context.Context, method string, payload interface{}, result interface{}) error {
	const maxRetries = 3
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		// handle backoff (skip for the first attempt)
		if i > 0 {
			wait := time.Duration(i*i) * time.Second
			s.client.logger.Warn("Retrying USGS API", "method", method, "attempt", i+1, "wait", wait)

			// ensure we don't sleep if the context is cancelled
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}

		// delegate the actual HTTP work to the Client "Engine"
		err := s.client.doRequest(ctx, method, payload, result)
		s.client.logger.Info("request.doRequest", "error", err)
		if err == nil {
			return nil
		}

		lastErr = err

		// isRetryable should check for things like 500 errors or timeouts.
		// 400 (Bad Request) should usually NOT be retried.
		if !isRetryable(err) {
			break
		}
	}

	return fmt.Errorf("%s failed after %d tries: %w", method, maxRetries, lastErr)
}

func (s *RequestService) RemoveSceneList(ctx context.Context, listId string) error {
	req := map[string]string{"listId": listId}
	// We don't need the result, just a check for API errors
	var resp BaseResponse
	return s.doRequest(ctx, "scene-list-remove", req, &resp)
}

// AddToSceneListSafely adds IDs one-by-one to avoid "all-or-nothing" failures.
// It returns a slice of IDs that were successfully added.
func (s *RequestService) AddToSceneListSafely(ctx context.Context, dataset, listId string, ids []string) []string {
	var confirmed []string
	var resp SceneListAddResponse

	for _, id := range ids {
		req := SceneListAddRequest{
			ListId:      listId,
			DatasetName: dataset,
			IdField:     "entityId",
			EntityId:    id,
			EntityIds:   []string{}, // USGS expects this empty if EntityId is set
		}

		// doRequest will automatically handle retries if the USGS
		// server has a 500-series "hiccup"
		err := s.doRequest(ctx, "scene-list-add", req, &resp)
		s.client.logger.Info("scene-list-add", "error", err)
		if err != nil {
			// if it's a 400 (Invalid ID), doRequest returns it,
			// and we log it here.
			s.client.logger.Warn("Skipping scene ID",
				"id", id,
				"reason", "ID not found or invalid for this dataset",
			)
			continue
		}
		confirmed = append(confirmed, id)
	}

	return confirmed
}

func isRetryable(err error) bool {
	if err == nil {
		return false
	}

	// check for Timeouts
	// this covers both network-level timeouts and context deadlines.
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	// check for Specific "Blips"
	// connection reset by peer, broken pipe, or connection refused.
	errStr := err.Error()
	if strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "EOF") {
		return true
	}

	// check for USGS Server-Side errors (5xx)
	if strings.Contains(errStr, "status: 500") ||
		strings.Contains(errStr, "status: 502") ||
		strings.Contains(errStr, "status: 503") ||
		strings.Contains(errStr, "status: 504") {
		return true
	}

	// default: Fail fast on 4xx (Bad Request, Unauthorized, etc.)
	return false
}

// GetDownloadURLs takes a list of entity IDs and returns the direct download links.
// This is the "Bridge" to the DownloadManager.
func (s *RequestService) GetDownloadURLs(ctx context.Context, items []DownloadRequestItem) (map[string]string, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("no download items provided")
	}

	// Prepare the Download Request Item
	req := map[string][]DownloadRequestItem{
		// "downloadApplication": "M2M",
		"downloads": items,
	}

	var resp DownloadRequestResponse

	// submit
	err := s.doRequest(ctx, "download-request", req, &resp)
	if err != nil {
		return nil, err
	}

	// map the results
	s.client.logger.Info("download request response", "resp", resp)
	links := make(map[string]string)
	for _, d := range resp.Data.Available {
		links[d.EntityId] = d.DownloadUrl
	}

	return links, nil
}

func (s *RequestService) GetDownloadOptions(ctx context.Context, dataset string, entityIds []string) ([]DownloadOption, error) {
	req := map[string]interface{}{
		"datasetName": dataset,
		"entityIds":   entityIds,
	}

	var resp struct {
		Data []DownloadOption `json:"data"`
	}

	err := s.doRequest(ctx, "download-options", req, &resp)
	if err != nil {
		return nil, err
	}
	s.client.logger.Info("download options return", "data", resp.Data)

	var allOptions []DownloadOption
	for _, opt := range resp.Data {
		// flatten the recursive structure
		allOptions = append(allOptions, flattenOptions(opt)...)
	}

	return allOptions, nil
}

func (s *RequestService) FilterForBundles(options []DownloadOption) []DownloadRequestItem {
	var items []DownloadRequestItem

	for _, opt := range options {
		if strings.Contains(opt.ProductName, "Bundle") {
			items = append(items, DownloadRequestItem{
				EntityId:  opt.EntityId,
				ProductId: opt.Id,
			})
		}
	}

	return items
}

func (s *RequestService) FilterForZip(options []DownloadOption) []DownloadRequestItem {
	var items []DownloadRequestItem

	for _, opt := range options {
		if strings.Contains(opt.DownloadSystem, "ls_zip") {
			items = append(items, DownloadRequestItem{
				EntityId:  opt.EntityId,
				ProductId: opt.Id,
			})
		}
	}

	return items
}

func (s *RequestService) FilterOutDirs(options []DownloadOption) []DownloadRequestItem {
	var items []DownloadRequestItem

	for _, opt := range options {
		if opt.DownloadSystem != "folder" {
			items = append(items, DownloadRequestItem{
				EntityId:  opt.EntityId,
				ProductId: opt.Id,
			})
		}
	}

	return items
}

func (s *RequestService) FilterForPreviews(options []DownloadOption) []DownloadRequestItem {
	var items []DownloadRequestItem
	for _, opt := range options {
		// 'Natural Color' or 'Full Resolution Browse'
		if strings.Contains(opt.ProductName, "Natural Color") ||
			strings.Contains(opt.ProductName, "Browse") {
			items = append(items, DownloadRequestItem{
				EntityId:  opt.EntityId,
				ProductId: opt.Id,
			})
		}
	}
	return items
}

func (s *RequestService) FilterForMetadata(options []DownloadOption) []DownloadRequestItem {
	var items []DownloadRequestItem
	for _, opt := range options {
		if strings.Contains(opt.ProductName, "Metadata") ||
			strings.Contains(opt.ProductName, "XML") {
			items = append(items, DownloadRequestItem{
				EntityId:  opt.EntityId,
				ProductId: opt.Id,
			})
		}
	}
	return items
}

func (s *RequestService) WaitUntilReady(ctx context.Context, ids []int64, label string, app string) error {
	// track what we've already sent to the Downloader to avoid double-queueing
	queued := make(map[int64]bool)

	s.client.logger.Info("Starting preparation poller", "label", label, "total_ids", len(ids))

	for {
		// immediate Context Check
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// poll the USGS API
		// this uses the s.doRequest we built with retries and auto-login
		result, err := s.DownloadRetrieve(ctx, ids, label, app)
		if err != nil {
			return fmt.Errorf("polling status failed: %w", err)
		}

		// dispatch Available Files
		for _, d := range result.Available {
			if !queued[d.DownloadId] {
				job := DownloadJob{
					EntityId: d.EntityId,
					URL:      d.DownloadUrl,
				}

				// enqueue into our Pond-backed worker pool
				s.client.Downloader.Enqueue(ctx, job)

				queued[d.DownloadId] = true
				s.client.logger.Info("File ready - dispatched to queue", "entityId", d.EntityId)
			}
		}

		// check if we are finished
		// if 'Requested' is empty, USGS has finished preparing all files in this batch
		if len(result.Requested) == 0 {
			s.client.logger.Info("All requested files have been prepared and queued", "label", label)
			return nil
		}

		// being patient
		s.client.logger.Info("Waiting for USGS preparation",
			"preparing_count", len(result.Requested),
			"next_poll", "60s")

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(60 * time.Second):
			// continue loop
		}
	}
}

// Recursive helper to find all "Leaf" products
func flattenOptions(opt DownloadOption) []DownloadOption {
	var result []DownloadOption

	// if it's a direct file (has a ProductCode and is Available)
	if opt.ProductCode != "" && opt.Available {
		result = append(result, opt)
	}

	// check for children
	for _, sub := range opt.SecondaryDownloads {
		result = append(result, flattenOptions(sub)...)
	}

	return result
}
