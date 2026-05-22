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

	// Range boundaries (populated when Operand == "BETWEEN")
	FirstValue  string
	SecondValue string
}

// parseMetaFlag converts strings like "WRS Path=90" or "WRS Path=90:95" into discrete query parts
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

			// --- Range Detection ---
			// If the parsed value contains a colon, upgrade this input to a BETWEEN operand
			if strings.Contains(val, ":") {
				valParts := strings.SplitN(val, ":", 2)
				return MetadataInput{
					FieldName:   field,
					Operand:     "between", // Dynamically swap operator context
					FirstValue:  strings.TrimSpace(valParts[0]),
					SecondValue: strings.TrimSpace(valParts[1]),
				}, nil
			}
			// ------------------------------------

			return MetadataInput{
				FieldName: field,
				Operand:   op,
				Value:     val,
			}, nil
		}
	}

	return MetadataInput{}, fmt.Errorf("no valid operator found in filter '%s' (supported: =, like)", raw)
}
