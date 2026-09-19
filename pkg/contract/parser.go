package contract

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// DECISION: Enforce DisallowUnknownFields during JSON deserialization.
// WHY: Prevent silent misconfigurations and typo regressions in production infrastructure contracts.
// TRADE-OFF: Forward compatibility requires schema updates before new fields can be parsed.
// REF: wiki/projects/izdeploy/izdeploy_engineering_backlog.md#task-006

// SyntaxError captures line-precise JSON parsing failures for developer ergonomics.
type SyntaxError struct {
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Message string `json:"message"`
}

// Error formats the syntax error into a concise diagnostic string.
func (e *SyntaxError) Error() string {
	return fmt.Sprintf("line %d, column %d: %s", e.Line, e.Column, e.Message)
}

// ParseConfigFile reads and validates the structure of an izDeploy JSON contract from disk.
//
// Business rule: The default location is .agent/izdeploy.json relative to project root.
//
// @ai-constraint: Must cleanly report missing files without panicking.
func ParseConfigFile(filePath string) (*AppConfig, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("configuration file not found: %s", filePath)
		}
		return nil, fmt.Errorf("failed reading configuration file %s: %w", filePath, err)
	}

	return ParseConfigBytes(data)
}

// ParseConfigBytes parses raw bytes into AppConfig with strict field validation and line detection.
//
// Business rule: Rejects unknown properties to keep contract manifests lean and deterministic.
//
// @ai-constraint: Calculates line and column offsets accurately when syntax violations occur.
func ParseConfigBytes(data []byte) (*AppConfig, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, errors.New("configuration payload is empty")
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var cfg AppConfig
	if err := decoder.Decode(&cfg); err != nil {
		var syntaxErr *json.SyntaxError
		if errors.As(err, &syntaxErr) {
			line, col := calculateLineCol(data, syntaxErr.Offset)
			return nil, &SyntaxError{
				Line:    line,
				Column:  col,
				Message: syntaxErr.Error(),
			}
		}

		var unmarshalTypeErr *json.UnmarshalTypeError
		if errors.As(err, &unmarshalTypeErr) {
			line, col := calculateLineCol(data, unmarshalTypeErr.Offset)
			return nil, &SyntaxError{
				Line:    line,
				Column:  col,
				Message: fmt.Sprintf("invalid type for field %q: expected %s, got %s", unmarshalTypeErr.Field, unmarshalTypeErr.Type, unmarshalTypeErr.Value),
			}
		}

		// Check for unknown field error from DisallowUnknownFields
		if strings.HasPrefix(err.Error(), "json: unknown field ") {
			fieldName := strings.TrimPrefix(err.Error(), "json: unknown field ")
			return nil, fmt.Errorf("unknown property %s is not permitted by schema v0.1", fieldName)
		}

		return nil, fmt.Errorf("json decoding error: %w", err)
	}

	// Verify no trailing extra tokens
	if err := decoder.Decode(&struct{}{}); err != io.EOF && err != nil {
		return nil, errors.New("configuration file contains extraneous trailing tokens")
	}

	return &cfg, nil
}

// calculateLineCol determines 1-indexed line and column numbers from byte offset in input data.
//
// Business rule: Line endings support both LF and CRLF formats uniformly.
//
// @ai-constraint: Offset cannot exceed length of data buffer.
func calculateLineCol(data []byte, offset int64) (int, int) {
	if offset > int64(len(data)) {
		offset = int64(len(data))
	}
	if offset < 0 {
		offset = 0
	}

	line := 1
	col := 1
	for i := int64(0); i < offset; i++ {
		if data[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}
