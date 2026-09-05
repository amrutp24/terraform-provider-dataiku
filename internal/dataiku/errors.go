package dataiku

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// APIError describes a non-2xx response from DSS.
type APIError struct {
	Method     string
	Path       string
	StatusCode int
	// Message is the DSS error message when the body was a recognizable
	// error envelope, otherwise the raw (truncated) body.
	Message string
	// Detail carries the DSS "detailedMessage"/"detailedMessageHTML" field.
	Detail string
}

func (e *APIError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s: HTTP %d %s", e.Method, e.Path, e.StatusCode, http.StatusText(e.StatusCode))
	if e.Message != "" {
		fmt.Fprintf(&b, ": %s", e.Message)
	}
	if e.Detail != "" && e.Detail != e.Message {
		fmt.Fprintf(&b, " (%s)", e.Detail)
	}
	return b.String()
}

// IsNotFound reports whether err is a 404 from DSS. Resource Read methods use
// this to remove an object from state instead of failing the run.
func IsNotFound(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusNotFound
	}
	return false
}

// IsUnauthorized reports whether err is a 401 or 403 from DSS.
func IsUnauthorized(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden
	}
	return false
}

// StatusCode returns the HTTP status carried by err, or 0 if err is not an
// APIError.
func StatusCode(err error) int {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode
	}
	return 0
}

func newAPIError(method, path string, resp *http.Response) *APIError {
	apiErr := &APIError{Method: method, Path: path, StatusCode: resp.StatusCode}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || len(strings.TrimSpace(string(raw))) == 0 {
		return apiErr
	}

	// DSS returns {"errorType":..., "message":..., "detailedMessage":...}
	// for most failures, but falls back to HTML for some gateway errors.
	var envelope struct {
		Message         string `json:"message"`
		DetailedMessage string `json:"detailedMessage"`
		ErrorType       string `json:"errorType"`
	}
	if json.Unmarshal(raw, &envelope) == nil && (envelope.Message != "" || envelope.ErrorType != "") {
		apiErr.Message = envelope.Message
		if apiErr.Message == "" {
			apiErr.Message = envelope.ErrorType
		}
		apiErr.Detail = envelope.DetailedMessage
		return apiErr
	}

	apiErr.Message = truncate(strings.TrimSpace(string(raw)), 512)
	return apiErr
}
