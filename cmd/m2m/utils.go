package main

import (
	"fmt"
	"strings"
)

// MetadataInput represents a raw user filter parsed from CLI string arguments
type MetadataInput struct {
	FieldName string
	Operand   string
	Value     string
}

// parseMetaFlag converts strings like "WRS Path=90" into discrete query parts
func parseMetaFlag(raw string) (MetadataInput, error) {
	// Order matters: match multi-character operators before single ones if we add more later
	operators := []string{"=", "like"}

	for _, op := range operators {
		if idx := strings.Index(raw, op); idx != -1 {
			field := strings.TrimSpace(raw[:idx])
			val := strings.TrimSpace(raw[idx+len(op):])

			if field == "" || val == "" {
				return MetadataInput{}, fmt.Errorf("invalid meta filter format '%s' (missing field or value)", raw)
			}

			return MetadataInput{
				FieldName: field,
				Operand:   op,
				Value:     val,
			}, nil
		}
	}

	return MetadataInput{}, fmt.Errorf("no valid operator found in filter '%s' (supported: =, like)", raw)
}
