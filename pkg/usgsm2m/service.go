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

// GetDownloadURLs takes a list of download items and returns their direct download links.
// If the USGS M2M API flags any entities as preparing/staging, this function will automatically
// poll the 'download-retrieve' endpoint until all items are fully cooked and available.
func (s *RequestService) GetDownloadURLs(ctx context.Context, items []DownloadRequestItem) (map[string]string, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("no download items provided")
	}

	// Generate a unique tracking label for this run's download transaction batch
	batchLabel := fmt.Sprintf("m2m_ingest_%d", time.Now().Unix())

	// 1. Submit the initial structural batch request with our tracking label
	resp, err := s.DownloadRequest(ctx, items, batchLabel)
	if err != nil {
		return nil, fmt.Errorf("failed to submit download request: %w", err)
	}

	links := make(map[string]string)

	// Harvest anything resting in active hot storage immediately
	for _, d := range resp.Data.Available {
		links[d.EntityId] = d.Url
	}

	// 2. If entities are stuck staging, loop and poll using our unique batch label
	if len(resp.Data.Preparing) > 0 {
		s.client.logger.Info(
			"M2M system is preparing entities for download. Awaiting staging infrastructure...",
			"preparing_count", len(resp.Data.Preparing),
			"batch_label", batchLabel,
		)

		for {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(15 * time.Second): // Give the USGS hardware time to pull files
			}

			// Poll the retrieval status passing our tracking batch label
			retrieveResp, err := s.DownloadRetrieve(ctx, batchLabel)
			if err != nil {
				return nil, fmt.Errorf("failed during staging retrieval: %w", err)
			}

			// Harvest newly available download endpoints
			for _, d := range retrieveResp.Data.Available {
				if _, exists := links[d.EntityId]; !exists {
					links[d.EntityId] = d.Url
				}
			}

			// Break out once the staging queue for our label context drops to zero
			remaining := len(retrieveResp.Data.Preparing)
			if remaining == 0 {
				s.client.logger.Info("All entities successfully staged and ready for transfer.")
				break
			}

			s.client.logger.Info(
				"Scenes are still preparing on USGS servers. Retrying shortly...",
				"remaining_count", remaining,
			)
		}
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

// FilterBySystem filters products based on an explicit target download system (e.g., "dds", "ls_zip").
// If targetSystem is empty, it falls back to selecting the first immediately available product per scene.
func (s *RequestService) FilterBySystem(options []DownloadOption, targetSystem string) []DownloadRequestItem {
	var items []DownloadRequestItem

	// track which entities we've already matched to prevent submitting duplicate
	// requests for the same scene if multiple files match the criteria.
	seenEntities := make(map[string]bool)

	useDefaultFallback := targetSystem == ""

	for _, opt := range options {
		// if the file isn't immediately downloadable, or we've already picked
		// a product for this scene, skip it.
		if !opt.Available || seenEntities[opt.EntityId] {
			continue
		}

		match := false
		if useDefaultFallback {
			// fallback: grab the primary production asset available for immediate download
			match = true
		} else if strings.EqualFold(opt.DownloadSystem, targetSystem) {
			match = true
		}

		if match {
			items = append(items, DownloadRequestItem{
				EntityId:  opt.EntityId,
				ProductId: opt.Id,
			})
			seenEntities[opt.EntityId] = true
		}
	}

	return items
}

// TODO: This function is currently deprecated and superseded by the unified,
// wait-first pipeline in GetDownloadURLs().
//
// Historically, WaitUntilReady was an active streaming poller designed to feed
// incoming download links directly into a background Downloader queue as they
// became available on the USGS staging tier, rather than waiting for the entire
// batch to finish cooking. It remains here as an architectural reference for
// dynamic, low-memory streaming workloads if session sizes scale past memory limits.
//
// func (s *RequestService) WaitUntilReady(ctx context.Context, ids []int64, label string, app string) error { ... }

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
