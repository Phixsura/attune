// SPDX-License-Identifier: Apache-2.0

// Package errorcode provides standardized error codes for API responses.
package errorcode

// Error codes for API responses.
const (
	// Client errors (4xx)
	BadRequest          = "bad_request"
	Unauthorized        = "unauthorized"
	Forbidden           = "forbidden"
	NotFound            = "not_found"
	MethodNotAllowed    = "method_not_allowed"
	Conflict            = "conflict"
	Gone                = "gone"
	PayloadTooLarge     = "payload_too_large"
	UnprocessableEntity = "unprocessable_entity"
	TooManyRequests     = "too_many_requests"

	// Validation errors
	ValidationFailed = "validation_failed"
	InvalidInput     = "invalid_input"
	MissingField     = "missing_field"
	InvalidFormat    = "invalid_format"
	OutOfRange       = "out_of_range"

	// Authentication errors
	InvalidCredentials = "invalid_credentials"
	TokenExpired       = "token_expired"
	TokenInvalid       = "token_invalid"
	SessionExpired     = "session_expired"

	// Authorization errors
	InsufficientScope      = "insufficient_scope"
	InsufficientPermission = "insufficient_permission"
	AccessDenied           = "access_denied"

	// Resource errors
	ResourceNotFound = "resource_not_found"
	ResourceExists   = "resource_exists"
	ResourceConflict = "resource_conflict"
	ResourceLocked   = "resource_locked"
	ResourceDeleted  = "resource_deleted"

	// Rate limiting
	RateLimitExceeded = "rate_limit_exceeded"
	QuotaExceeded     = "quota_exceeded"

	// Server errors (5xx)
	InternalError      = "internal_error"
	ServiceUnavailable = "service_unavailable"
	Timeout            = "timeout"
	DatabaseError      = "database_error"
	ExternalError      = "external_error"

	// Business logic errors
	OperationFailed    = "operation_failed"
	PreconditionFailed = "precondition_failed"
	StateConflict      = "state_conflict"
)

// ErrorResponse represents a standardized API error response.
type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

// New creates a new ErrorResponse.
func New(code, message string) ErrorResponse {
	return ErrorResponse{Code: code, Message: message}
}

// WithDetails adds details to an ErrorResponse.
func (e ErrorResponse) WithDetails(details any) ErrorResponse {
	e.Details = details
	return e
}
