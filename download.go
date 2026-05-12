package usgsm2m

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alitto/pond/v2"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

type DownloadManager struct {
	client    *Client
	pool      pond.Pool
	progress  *mpb.Progress
	logger    *slog.Logger // borrowed from Client
	outputDir string
}

func NewDownloadManager(client *Client, workers int, outputDir string) (*DownloadManager, error) {
	// attempt to create the directory
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("could not create output directory %s: %w", outputDir, err)
	}

	// verify write permissions with a temporary file
	testFile := filepath.Join(outputDir, ".write_test")
	if err := os.WriteFile(testFile, []byte("write test"), 0644); err != nil {
		return nil, fmt.Errorf("output directory %s is not writable: %w", outputDir, err)
	}
	os.Remove(testFile)

	dm := DownloadManager{
		client:    client,
		pool:      pond.NewPool(workers),
		progress:  mpb.New(mpb.WithWidth(64)),
		logger:    client.logger,
		outputDir: outputDir,
	}

	return &dm, nil
}

func (d *DownloadManager) Enqueue(ctx context.Context, job DownloadJob) {
	d.pool.Submit(func() {
		// pre-run check: eg "Did the user cancel while we were in the queue?"
		select {
		case <-ctx.Done():
			d.logger.Debug("Task cancelled before starting", "file", job.EntityId)
			return
		default:
			// proceed normally
		}

		// pass the context DOWN into the retry/download logic
		if err := d.DownloadWithRetry(ctx, job); err != nil {
			d.logger.Error("Download failed", "file", job.EntityId, "error", err)
		}
	})
}

func (m *DownloadManager) DownloadWithRetry(ctx context.Context, job DownloadJob) error {
	const maxRetries = 3
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			wait := time.Duration(math.Pow(2, float64(i))) * time.Second
			m.client.logger.Warn("Retrying download",
				"entityId", job.EntityId,
				"attempt", i+1,
				"wait", wait,
			)

			// modern Go: Sleep but stay alert for Context cancellation
			select {
			case <-time.After(wait):
				// sleep finished, proceed to retry
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		err := m.downloadFile(ctx, job)
		if err == nil {
			return nil // success!
		}

		lastErr = err

		// logic gate: if it's a permanent error, don't waste time retrying
		if !isDownloadRetryable(err) {
			break
		}
	}

	return fmt.Errorf("download failed after %d attempts: %w", maxRetries, lastErr)
}

func isDownloadRetryable(err error) bool {
	// don't retry if the file simply isn't there or we aren't allowed to see it
	if strings.Contains(err.Error(), "status 404") ||
		strings.Contains(err.Error(), "status 401") ||
		strings.Contains(err.Error(), "status 403") {
		return false
	}
	return true // retry on everything else (timeouts, 500s, EOFs)
}

func (d *DownloadManager) downloadFile(ctx context.Context, job DownloadJob) (err error) {
	req, err := http.NewRequestWithContext(ctx, "GET", job.URL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := d.client.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("network error: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return fmt.Errorf("server returned status %d for %s", resp.StatusCode, job.EntityId)
	}

	// create a way to stop the killer goroutine if the download finishes naturally
	downloadDone := make(chan struct{})
	defer close(downloadDone)

	// ... [ProgressBar Setup Code] ...
	displayName := fmt.Sprintf("%s (%s)", job.EntityId, job.ProductName)
	bar := d.progress.AddBar(resp.ContentLength,
		mpb.PrependDecorators(
			decor.Name(displayName, decor.WC{W: len(displayName) + 1, C: decor.DindentRight}),
			decor.CountersKibiByte("% .2f / % .2f"),
		),
		mpb.AppendDecorators(
			decor.Percentage(decor.WC{W: 5}),
			decor.Name(" @ "),
			decor.AverageSpeed(decor.SizeB1024(0), "% .2f"),
		),
	)

	go func() {
		select {
		case <-ctx.Done():
			// tell the UI to give up
			bar.Abort(true)
			// kill the network stream
			resp.Body.Close()
		case <-downloadDone:
			// normal exit
		}
	}()

	proxyReader := bar.ProxyReader(resp.Body)
	defer proxyReader.Close()

	realFilename := GetFilenameFromHeaders(resp, job.EntityId+".tar.gz")
	productPath := filepath.Join(d.outputDir, job.ProductName)
	if err := os.MkdirAll(productPath, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", productPath, err)
	}

	finalPath := filepath.Join(productPath, realFilename)

	if _, err := os.Stat(finalPath); err == nil {
		d.client.logger.Info("File already exists, skipping", "file", realFilename)
		if bar != nil {
			bar.Abort(true) // 'true' removes it from the display immediately
		}
		return nil
	}

	tmpPath := finalPath + ".tmp"
	out, err := os.Create(tmpPath)
	// out, err := os.Create(finalPath)
	if err != nil {
		bar.Abort(true)
		return fmt.Errorf("could not create file %s: %w", tmpPath, err)
	}

	defer func() {
		out.Close()
		if err != nil {
			d.client.logger.Warn("Cleaning up failed download", "path", tmpPath, "error", err)
			os.Remove(tmpPath)
		}
	}()

	d.client.logger.Debug("Streaming bits to disk", "file", realFilename)

	// capture the error from io.Copy into our named return variable
	_, err = io.Copy(out, proxyReader)
	if err != nil {
		return err
	}

	if err := out.Close(); err != nil {
		return fmt.Errorf("failed to close file: %w", err)
	}

	// perform the Atomic Rename
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	// CRITICAL: return the error so the defer and retry logic can see it!
	// in this case, all the way to the end, we should have no error
	// and nil is returned instead
	return nil
}

type DownloadOptionsResponse struct {
	BaseResponse
	Data []DownloadOption `json:"data"`
}

func (s *RequestService) RequestDownload(ctx context.Context, codes []string, label string) (*DownloadResponse, error) {
	req := DownloadRequest{
		DownloadCodes: codes,
		Label:         label,
	}

	var resp DownloadResponse
	// BaseResponse engine handles error checking automatically!
	err := s.doRequest(ctx, "download-request", req, &resp)
	if err != nil {
		return nil, err
	}

	return &resp, nil
}

type DownloadRequest struct {
	// these IDs come from the DownloadOption.Id field
	DownloadCodes []string `json:"downloadCodes"`
	Label         string   `json:"label"`
}

type DownloadResponse struct {
	BaseResponse
	Data struct {
		// Files ready to pull right now
		AvailableDownloads []AvailableDownload `json:"availableDownloads"`
		// Files the USGS is fetching from deep storage
		PreparingDownloads []PreparingDownload `json:"preparingDownloads"`
		// Files you already requested in this session
		DuplicateProducts []string `json:"duplicateProducts"`
	} `json:"data"`
}

type DownloadRetrieveRequest struct {
	DownloadApplication string  `json:"downloadApplication,omitempty"`
	Label               string  `json:"label,omitempty"`
	DownloadIds         []int64 `json:"downloadIds,omitempty"`
}

// DownloadRetrieve checks the status of a previous DownloadRequest using its Label.
func (s *RequestService) DownloadRetrieve(ctx context.Context, ids []int64, app string, label string) (*DownloadRetrieveResult, error) {
	payload := DownloadRetrieveRequest{
		DownloadApplication: app,
		Label:               label,
		DownloadIds:         ids,
	}

	var resp DownloadRetrieveResponse
	err := s.client.doRequest(ctx, "download-retrieve", payload, &resp)
	if err != nil {
		return nil, err
	}

	return &resp.Data, nil
}

func (s *RequestService) SubmitDownloadRequest(ctx context.Context, payload DownloadRequestPayload) (DownloadRequestResponse, error) {
	var resp DownloadRequestResponse
	err := s.doRequest(ctx, "download-request", payload, &resp)
	return resp, err
}

func (d *DownloadManager) Wait() {
	d.logger.Info("Awaiting pool drain...")
	d.pool.StopAndWait() // Standard pond shutdown
	d.progress.Wait()    // Ensure bars finish rendering
}
