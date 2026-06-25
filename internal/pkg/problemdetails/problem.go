// SPDX-License-Identifier: Apache-2.0

// Package problemdetails implements RFC 7807 Problem Details for HTTP APIs.
package problemdetails

import (
	"encoding/json"
	"net/http"
)

// ContentType is the MIME type for Problem Details responses.
const ContentType = "application/problem+json"

// Problem represents an RFC 7807 Problem Details object.
type Problem struct {
	// Type is a URI reference that identifies the problem type.
	Type string `json:"type,omitempty"`

	// Title is a short, human-readable summary of the problem type.
	Title string `json:"title"`

	// Status is the HTTP status code.
	Status int `json:"status"`

	// Detail is a human-readable explanation specific to this occurrence.
	Detail string `json:"detail,omitempty"`

	// Instance is a URI reference that identifies the specific occurrence.
	Instance string `json:"instance,omitempty"`

	// Extensions contains additional members.
	Extensions map[string]any `json:"-"`
}

// MarshalJSON implements json.Marshaler with extensions flattened.
func (p Problem) MarshalJSON() ([]byte, error) {
	type plain Problem
	base, err := json.Marshal(plain(p))
	if err != nil {
		return nil, err
	}

	if len(p.Extensions) == 0 {
		return base, nil
	}

	var merged map[string]any
	if err := json.Unmarshal(base, &merged); err != nil {
		return nil, err
	}

	for k, v := range p.Extensions {
		merged[k] = v
	}

	return json.Marshal(merged)
}

// Write writes the Problem to the response writer.
func (p Problem) Write(w http.ResponseWriter) {
	w.Header().Set("Content-Type", ContentType)
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

// New creates a new Problem with the given status and title.
func New(status int, title string) Problem {
	return Problem{
		Type:   "about:blank",
		Status: status,
		Title:  title,
	}
}

// WithDetail adds a detail message.
func (p Problem) WithDetail(detail string) Problem {
	p.Detail = detail
	return p
}

// WithType sets the problem type URI.
func (p Problem) WithType(typeURI string) Problem {
	p.Type = typeURI
	return p
}

// WithInstance sets the instance URI.
func (p Problem) WithInstance(instance string) Problem {
	p.Instance = instance
	return p
}

// WithExtension adds an extension field.
func (p Problem) WithExtension(key string, value any) Problem {
	if p.Extensions == nil {
		p.Extensions = make(map[string]any)
	}
	p.Extensions[key] = value
	return p
}

// Common problem constructors

// BadRequest creates a 400 Bad Request problem.
func BadRequest(detail string) Problem {
	return New(http.StatusBadRequest, "Bad Request").WithDetail(detail)
}

// Unauthorized creates a 401 Unauthorized problem.
func Unauthorized(detail string) Problem {
	return New(http.StatusUnauthorized, "Unauthorized").WithDetail(detail)
}

// Forbidden creates a 403 Forbidden problem.
func Forbidden(detail string) Problem {
	return New(http.StatusForbidden, "Forbidden").WithDetail(detail)
}

// NotFound creates a 404 Not Found problem.
func NotFound(detail string) Problem {
	return New(http.StatusNotFound, "Not Found").WithDetail(detail)
}

// Conflict creates a 409 Conflict problem.
func Conflict(detail string) Problem {
	return New(http.StatusConflict, "Conflict").WithDetail(detail)
}

// TooManyRequests creates a 429 Too Many Requests problem.
func TooManyRequests(detail string, retryAfter int) Problem {
	return New(http.StatusTooManyRequests, "Too Many Requests").
		WithDetail(detail).
		WithExtension("retry_after", retryAfter)
}

// InternalError creates a 500 Internal Server Error problem.
func InternalError(detail string) Problem {
	return New(http.StatusInternalServerError, "Internal Server Error").WithDetail(detail)
}

// ServiceUnavailable creates a 503 Service Unavailable problem.
func ServiceUnavailable(detail string) Problem {
	return New(http.StatusServiceUnavailable, "Service Unavailable").WithDetail(detail)
}
