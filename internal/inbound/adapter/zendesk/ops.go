// SPDX-License-Identifier: Apache-2.0

package zendesk

import (
	"errors"
	"strings"
)

// isPermanentZendeskError reports whether a Zendesk API error should
// disable the source instead of being retried.
func isPermanentZendeskError(err error) bool {
	var ae apiError
	if errors.As(err, &ae) {
		return ae.Permanent()
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{
		"unauthorized",
		"forbidden",
		"couldn't authenticate you",
		"invalid credentials",
		"invalid_credentials",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

// isDuplicateError checks if an ingest error is an idempotency-key conflict.
func isDuplicateError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "idempotency key used with different request")
}
