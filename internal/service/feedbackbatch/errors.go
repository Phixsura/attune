// SPDX-License-Identifier: Apache-2.0

package feedbackbatch

import "errors"

// Service-level errors for batch operations. These map to proto error codes
// in the handler layer.
var (
	// ErrRateLimited signals the tenant has exceeded their rate limit.
	ErrRateLimited = errors.New("rate limit exceeded")

	// ErrIdempotencyConflict signals the idempotency key exists but was used
	// with different request parameters (different hash).
	ErrIdempotencyConflict = errors.New("idempotency key conflict")

	// ErrRequestInProgress signals the idempotency key is currently being
	// processed by another request.
	ErrRequestInProgress = errors.New("request in progress")

	// ErrCountMismatch signals that the actual count of matched items differs
	// from the confirm_count safety check provided by the client.
	ErrCountMismatch = errors.New("actual count does not match confirm_count")

	// ErrExceedsMaxAffected signals that a query matches more items than the
	// max_affected safety cap allows.
	ErrExceedsMaxAffected = errors.New("query matches more items than max_affected")

	// ErrConcurrentJobLimit signals the tenant has reached their limit of
	// concurrent async batch jobs.
	ErrConcurrentJobLimit = errors.New("concurrent job limit reached")

	// ErrVersionConflict signals a feedback row was modified after the
	// if_unmodified_since timestamp.
	ErrVersionConflict = errors.New("feedback has been modified")

	// ErrNoOperation signals the request did not specify an operation.
	ErrNoOperation = errors.New("no operation specified")

	// ErrNoSelection signals neither feedback_ids nor query was provided.
	ErrNoSelection = errors.New("neither feedback_ids nor query provided")

	// ErrMixedSelection signals both feedback_ids and query were provided.
	ErrMixedSelection = errors.New("cannot specify both feedback_ids and query")

	// ErrJobNotFound signals the async job ID does not exist for the tenant.
	ErrJobNotFound = errors.New("job not found")

	// ErrJobNotCancellable signals the job cannot be cancelled in its current state.
	ErrJobNotCancellable = errors.New("job cannot be cancelled")
)
