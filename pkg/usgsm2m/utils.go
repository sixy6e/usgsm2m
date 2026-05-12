package usgsm2m

import (
	"fmt"
	"math/rand"
	"mime"
	"net/http"
	"os"
)

// GetFilenameFromHeaders attempts to find the official filename in the response.
// If it fails, it falls back to a provided default name.
func GetFilenameFromHeaders(resp *http.Response, defaultName string) string {
	contentDisp := resp.Header.Get("Content-Disposition")
	if contentDisp == "" {
		return defaultName
	}

	// mime.ParseMediaType handles quotes and escaped characters automatically
	_, params, err := mime.ParseMediaType(contentDisp)
	if err != nil {
		return defaultName
	}

	if filename, ok := params["filename"]; ok {
		return filename
	}

	// fallback for extended parameter syntax
	if filename, ok := params["filename*"]; ok {
		return filename
	}

	return defaultName
}

// BatchSlice splits a slice of any type into a slice of slices of a specific size.
func BatchSlice[T any](items []T, batchSize int) [][]T {
	var batches [][]T
	for i := 0; i < len(items); i += batchSize {
		end := i + batchSize

		// ensure we don't go out of bounds on the last (shorter) batch
		if end > len(items) {
			end = len(items)
		}

		batches = append(batches, items[i:end])
	}
	return batches
}

// FileExists checks if a file or directory exists.
func FileExists(filename string) bool {
	info, err := os.Stat(filename)
	if err == nil {
		// ensure it's not an empty file
		return info.Size() > 0
	}
	return false
}

func GenerateBatchId() string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 6)
	for i := range b {
		// using math/rand is fine for a non-security identifier
		b[i] = charset[rand.Intn(len(charset))]
	}
	return fmt.Sprintf("batch-%s", string(b))
}
