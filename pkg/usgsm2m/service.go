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

// ResponseEnvelope represents the standard outer shell returned by every USGS M2M API endpoint.
// The Data field dynamically morphs into whatever inner struct payload you pass to it.
type ResponseEnvelope[T any] struct {
	Version      string  `json:"version"`
	ErrorCode    *string `json:"errorCode"`
	ErrorMessage string  `json:"errorMessage"`
	RequestID    int64   `json:"requestId"`
	SessionID    *int64  `json:"sessionId"`
	Data         T       `json:"data"`
}

// Error checks if the USGS API returned an explicit error payload.
// We name it Error() so it naturally converts into a standard Go error type when needed!
func (e *ResponseEnvelope[T]) Error() error {
	if e.ErrorCode != nil && *e.ErrorCode != "" {
		return fmt.Errorf("USGS Error (%s): %s", *e.ErrorCode, e.ErrorMessage)
	}
	return nil
}

// Cleanup removes the temporary scene list from the USGS server.
func (s *RequestService) Cleanup(ctx context.Context, listId string) {
	if listId == "" {
		return
	}

	req := map[string]string{
		"listId": listId,
	}

	// var resp BaseResponse
	var data struct{}

	// use a background-style log here because the job is technically "done"
	err := doRequest(ctx, s, "scene-list-remove", req, &data)
	if err != nil {
		s.client.logger.Warn("Failed to clean up remote scene list", "listId", listId, "err", err)
	} else {
		s.client.logger.Info("Successfully removed remote scene list", "listId", listId)
	}
}

// doRequest wraps the client transport logic, intercepts USGS-specific errors,
// unmarshals into our generic response envelope, and manages backoffs.
//
// T represents the inner "data" struct type expected from the USGS endpoint.
func doRequest[T any](ctx context.Context, s *RequestService, method string, payload interface{}, result *T) error {
	const maxRetries = 3
	var (
		lastErr error
		i       int
	)

	for i = 0; i < maxRetries; i++ {
		// handle backoff (skip for the first attempt)
		if i > 0 {
			wait := time.Duration(i*i) * time.Second

			// SPECIAL CASE: if the previous error was a rate limit,
			// force a heavy penalty wait to let the USGS database connections clear.
			if lastErr != nil && strings.Contains(lastErr.Error(), "RATE_LIMIT") {
				wait = 5 * time.Second
			}

			s.client.logger.Warn("Retrying USGS API", "method", method, "attempt", i+1, "wait", wait)

			// ensure we don't sleep if the context is cancelled
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}

		// create a fresh instance of the generic envelope tailored to the expected data shape 'T'
		var envelope ResponseEnvelope[T]

		// delegate the actual HTTP work to the Client "Engine"
		err := s.client.doRequest(ctx, method, payload, &envelope)
		if err != nil {
			lastErr = err
			s.client.logger.Info("Request attempt failed", "method", method, "attempt", i+1, "error", err)

			// Check if it's structural (like bad JSON data) or fatal.
			// If it's a RATE_LIMIT, it returns true, allowing us to drop into the next loop
			// where it hits our 5-second backoff rule above.
			// isRetryable should check for things like 500 errors or timeouts.
			// 400 (Bad Request) should usually NOT be retried.
			if !isRetryable(err) {
				break
			}
			continue
		}

		// check for business/functional errors returned by the USGS API itself
		if apiErr := envelope.Error(); apiErr != nil {
			lastErr = apiErr
			s.client.logger.Info("USGS API returned business error", "method", method, "attempt", i+1, "error", apiErr)

			// if it's a RATE_LIMIT, continue the loop so it hits our penalty backoff rule above
			if envelope.ErrorCode != nil && *envelope.ErrorCode == "RATE_LIMIT" {
				continue
			}

			// for any other structural/fatal USGS error (like AUTH_FAILURE), fail-fast instantly
			break
		}

		// Success! Extract the unwrapped, strongly-typed data and assign it to the caller's target pointer
		*result = envelope.Data
		return nil
	}

	// Uses (i+1) instead of maxRetries so it accurately reflects how many attempts were made before breaking
	return fmt.Errorf("%s failed after %d attempts: %w", method, i+1, lastErr)
}

func (s *RequestService) RemoveSceneList(ctx context.Context, listId string) error {
	req := map[string]string{"listId": listId}
	// We don't need the result, just a check for API errors
	var data struct{}
	return doRequest(ctx, s, "scene-list-remove", req, &data)
}

// AddToSceneListSafely adds IDs one-by-one to avoid "all-or-nothing" failures.
// It returns a slice of IDs that were successfully added.
func (s *RequestService) AddToSceneListSafely(ctx context.Context, dataset, listId string, ids []string) []string {
	var confirmed []string
	var data SceneListAddData

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
		err := doRequest(ctx, s, "scene-list-add", req, &data)
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

// isRetryable evaluates an error to determine if it is a transient failure
// (like network timeouts, socket blips, server 5xx drops, or rate limits)
// that could succeed on a subsequent attempt, or a fatal structural failure
// (like bad payloads or 4xx auth errors) that should fail fast.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}

	// check for timeouts
	// this covers both network-level timeouts and context deadlines.
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	// check for specific socket "Blips"
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

	// EXPLICITLY allow retries for USGS rate limit scenarios.
	// combined with the 5-second backoff guard in doRequest,
	// this will successfully recover from heavy spatial database locks.
	if strings.Contains(errStr, "RATE_LIMIT") || strings.Contains(errStr, "status: 429") {
		return true
	}

	// default: fail fast on standard 4xx (Bad Request, Unauthorized, etc.)
	return false
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
