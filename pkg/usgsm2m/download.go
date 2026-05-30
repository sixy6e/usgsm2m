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

// AvailableDownload represents a file that has a URL ready for the worker pool
type AvailableDownload struct {
	DownloadId  int64  `json:"downloadId"`
	EntityId    string `json:"entityId"`
	Url         string `json:"url"`
	FileSize    int64  `json:"filesize"`
	ProductName string `json:"productName"` // Keep this for logging/validation
}

// PreparingDownload represents a file the USGS is currently fetching from tape/archive
type PreparingDownload struct {
	DownloadId int64  `json:"downloadId"`
	EntityId   string `json:"entityId"`
}

type FailedDownload struct {
	DownloadId   int64  `json:"downloadId"`
	EntityId     string `json:"entityId"`
	ErrorMessage string `json:"errorMessage"` // sometimes 'error' or 'message'
}

type DownloadRequestItem struct {
	ProductId string `json:"productId"`
	EntityId  string `json:"entityId"`
}

type DownloadRequestPayload struct {
	Downloads []DownloadRequestItem `json:"downloads"`
	Label     string                `json:"label,omitempty"` // Optional: name your "order"
}

// RequestedDownload represents a file that the USGS is still preparing
type RequestedDownload struct {
	DownloadId int64  `json:"downloadId"`
	EntityId   string `json:"entityId"`
}

// DownloadItem refers to a single downloadable item
type DownloadItem struct {
	DownloadId int64  `json:"downloadId"`
	EntityId   string `json:"entityId"`
	URL        string `json:"url,omitempty"` // URL is empty if still preparing
}

// DownloadRetrievePayload defines the filtering criteria for fetching staged download links.
// Maps to the USGS M2M 'download-retrieve' request endpoint.
type DownloadRetrievePayload struct {
	DownloadApplication string `json:"downloadApplication,omitempty"`
	Label               string `json:"label,omitempty"`
}

type DownloadRetrieveResult struct {
	Available []AvailableDownload `json:"available"`
	Requested []PreparingDownload `json:"requested"`
	QueueSize int64               `json:"queueSize"`
	Eulas     []Eula              `json:"eulas"`
}

// DownloadRequestData is the "Receipt" from the download-request endpoint
type DownloadRequestData struct {
	Available         []AvailableDownload `json:"availableDownloads"`
	Preparing         []PreparingDownload `json:"preparingDownloads"`
	Failed            []FailedDownload    `json:"failed"`
	DuplicateProducts DuplicateProducts   `json:"duplicateProducts"`
}

// DownloadOptionsRequest defines the parameters required by the USGS M2M
// download-options endpoint to discover downloadable products.
type DownloadOptionsRequest struct {
	DatasetName                string   `json:"datasetName"`
	EntityIds                  []string `json:"entityIds,omitempty"`
	ListID                     string   `json:"listId,omitempty"`
	IncludeSecondaryFileGroups *bool    `json:"includeSecondaryFileGroups,omitempty"`
}

type DownloadOption struct {
	Available      bool   `json:"available"`
	BulkAvailable  bool   `json:"bulkAvailable"`
	DatasetId      string `json:"datasetId"`
	DisplayId      string `json:"displayId"`
	DownloadName   string `json:"downloadName"`
	DownloadSystem string `json:"downloadSystem"`
	EntityId       string `json:"entityId"`
	Filesize       int64  `json:"filesize"`
	Id             string `json:"id"`
	ProductCode    string `json:"productCode"`
	ProductName    string `json:"productName"`
	// Recursive: some products have sub-products
	SecondaryDownloads []DownloadOption `json:"secondaryDownloads"`
}

type DownloadOptionList []DownloadOption

type DownloadJob struct {
	EntityId    string
	ProductName string
	URL         string
}

func (d *DownloadManager) Enqueue(ctx context.Context, job DownloadJob) {
	// CRITICAL FIX: create a locally scoped copy of the job
	// (one of Go's gotcha's)
	// this forces Go to allocate a unique block of memory for this specific function block,
	// preventing downstream pool workers from reading mutated loop data
	localJob := job

	d.pool.Submit(func() {
		// pre-run check: eg "Did the user cancel while we were in the queue?"
		select {
		case <-ctx.Done():
			d.logger.Debug("Task cancelled before starting", "file", localJob.EntityId)
			return
		default:
			// proceed normally
		}

		// pass the context DOWN into the retry/download logic
		if err := d.DownloadWithRetry(ctx, localJob); err != nil {
			d.logger.Error("Download failed", "file", localJob.EntityId, "error", err)
		}
	})
}

func (m *DownloadManager) DownloadWithRetry(ctx context.Context, job DownloadJob) error {
	const maxRetries = 3
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			// FIX: Check if the user killed the app BEFORE logging or calculating wait times
			if ctx.Err() != nil {
				return ctx.Err()
			}

			wait := time.Duration(math.Pow(2, float64(i))) * time.Second
			m.client.logger.Warn("Retrying download",
				"entityId", job.EntityId,
				"attempt", i+1,
				"wait", wait,
			)

			// sleep but stay alert for Context cancellation
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

	// create a way to stop the goroutine if the download finishes naturally
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
		if err != nil || ctx.Err() != nil {
			d.client.logger.Warn("Cleaning up failed download",
				"path", tmpPath,
				"error", err,
				"ctx_err", ctx.Err(),
			)
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

// DownloadRequest submits a list of product items to the M2M download pipeline using a tracking label.
// calls the 'download-request' M2M endpoint.
func (s *RequestService) DownloadRequest(ctx context.Context, items []DownloadRequestItem, label string) (*DownloadRequestData, error) {
	payload := DownloadRequestPayload{
		Downloads: items,
		Label:     label, // Attaching the label lets us isolate this exact batch later
	}

	var data DownloadRequestData
	err := doRequest(ctx, s, "download-request", payload, &data)
	if err != nil {
		return nil, err
	}

	return &data, nil
}

// DownloadRetrieve checks the status of specifically queued assets using their unique tracking label.
// Maps directly to the 'download-retrieve' M2M endpoint.
func (s *RequestService) DownloadRetrieve(ctx context.Context, label string) (*DownloadRequestData, error) {
	payload := DownloadRetrievePayload{
		Label:               label,
		DownloadApplication: "M2M",
	}

	var data DownloadRequestData
	err := doRequest(ctx, s, "download-retrieve", payload, &data)
	if err != nil {
		return nil, err
	}

	return &data, nil
}

// GetDownloadURLs takes a list of download items and returns their direct download links.
// If the USGS M2M API flags any entities as preparing/staging, this function will automatically
// poll the 'download-retrieve' endpoint until all items are fully cooked and available.
func (s *RequestService) GetDownloadURLs(ctx context.Context, items []DownloadRequestItem) (map[string]string, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("no download items provided")
	}

	// generate a unique tracking label for this run's download transaction batch
	batchLabel := fmt.Sprintf("m2m_ingest_%d", time.Now().Unix())

	// submit the initial structural batch request with our tracking label
	downloadRequestData, err := s.DownloadRequest(ctx, items, batchLabel)
	if err != nil {
		return nil, fmt.Errorf("failed to submit download request: %w", err)
	}

	links := make(map[string]string)

	// harvest anything resting in active hot storage immediately
	for _, d := range downloadRequestData.Available {
		links[d.EntityId] = d.Url
	}

	// if entities are stuck staging, loop and poll using our unique batch label
	if len(downloadRequestData.Preparing) > 0 {
		s.client.logger.Info(
			"M2M system is preparing entities for download. Awaiting staging infrastructure...",
			"preparing_count", len(downloadRequestData.Preparing),
			"batch_label", batchLabel,
		)

		for {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(s.pollInterval): // give the USGS hardware time to pull files
			}

			// poll the retrieval status passing our tracking batch label
			retrieveData, err := s.DownloadRetrieve(ctx, batchLabel)
			if err != nil {
				return nil, fmt.Errorf("failed during staging retrieval: %w", err)
			}

			// harvest newly available download endpoints
			for _, d := range retrieveData.Available {
				if _, exists := links[d.EntityId]; !exists {
					links[d.EntityId] = d.Url
				}
			}

			// break out once the staging queue for our label context drops to zero
			remaining := len(retrieveData.Preparing)
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
	req := DownloadOptionsRequest{DatasetName: dataset, EntityIds: entityIds}

	var data []DownloadOption

	err := doRequest(ctx, s, "download-options", req, &data)
	if err != nil {
		return nil, err
	}
	// s.client.logger.Info("download options return", "data", resp.Data)

	var allOptions []DownloadOption
	for _, opt := range data {
		// flatten the recursive structure
		allOptions = append(allOptions, flattenOptions(opt)...)
	}

	return allOptions, nil
}

func FilterForBundles(options []DownloadOption) []DownloadRequestItem {
	return filterOptions(options, func(opt DownloadOption) bool {
		return strings.Contains(opt.ProductName, "Bundle")
	})
}

func FilterForZip(options []DownloadOption) []DownloadRequestItem {
	return filterOptions(options, func(opt DownloadOption) bool {
		return strings.Contains(opt.DownloadSystem, "ls_zip")
	})
}

func FilterOutDirs(options []DownloadOption) []DownloadRequestItem {
	return filterOptions(options, func(opt DownloadOption) bool {
		return opt.DownloadSystem != "folder"
	})
}

func FilterForPreviews(options []DownloadOption) []DownloadRequestItem {
	return filterOptions(options, func(opt DownloadOption) bool {
		return strings.Contains(opt.ProductName, "Natural Color") || strings.Contains(opt.ProductName, "Browse")
	})
}

func FilterForMetadata(options []DownloadOption) []DownloadRequestItem {
	return filterOptions(options, func(opt DownloadOption) bool {
		return strings.Contains(opt.ProductName, "Metadata") || strings.Contains(opt.ProductName, "XML")
	})
}

// filterOptions is the core engine function that does the heavy lifting for
// download option filtering
func filterOptions(options []DownloadOption, predicate func(DownloadOption) bool) []DownloadRequestItem {
	var items []DownloadRequestItem
	for _, opt := range options {
		if predicate(opt) {
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
func FilterBySystem(options []DownloadOption, targetSystem string) []DownloadRequestItem {
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

// func (s *RequestService) SubmitDownloadRequest(ctx context.Context, payload DownloadRequestPayload) (DownloadRequestResponse, error) {
// 	var resp DownloadRequestResponse
// 	err := s.doRequest(ctx, "download-request", payload, &resp)
// 	return resp, err
// }

func (d *DownloadManager) Wait() {
	d.logger.Info("Awaiting pool drain...")
	d.pool.StopAndWait() // Standard pond shutdown
	d.progress.Wait()    // Ensure bars finish rendering
}
