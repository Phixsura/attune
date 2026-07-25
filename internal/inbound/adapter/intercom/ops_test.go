// SPDX-License-Identifier: Apache-2.0

package intercom

import (
	"errors"
	"testing"
)

func TestIsPermanentIntercomError(t *testing.T) {
	permanent := []error{
		apiError{Status: 401, Code: "unauthorized"},
		apiError{Status: 403, Code: "api_plan_restricted"},
		apiError{Status: 403, Code: "forbidden"},
		errors.New("Access Token Invalid"),
		errors.New("wrapped: token_revoked"),
	}
	for _, err := range permanent {
		if !isPermanentIntercomError(err) {
			t.Errorf("%v should be permanent", err)
		}
	}

	transient := []error{
		apiError{Status: 500, Code: "server_error"},
		apiError{Status: 502, Code: "bad gateway"},
		rateLimitError{Method: "/x"},
		errors.New("connection refused"),
		apiError{Status: 404, Code: "not_found"},
	}
	for _, err := range transient {
		if isPermanentIntercomError(err) {
			t.Errorf("%v should NOT be permanent", err)
		}
	}
}

func TestIsDuplicateError(t *testing.T) {
	if !isDuplicateError(errors.New("Idempotency key used with different request body")) {
		t.Error("duplicate not detected")
	}
	if isDuplicateError(errors.New("content is required")) {
		t.Error("false positive")
	}
}

func TestIsPermanentError_Exported(t *testing.T) {
	if !IsPermanentError(apiError{Status: 401, Code: "unauthorized"}) {
		t.Error("exported wrapper broken")
	}
}
