// SPDX-License-Identifier: Apache-2.0

// ptrext:file-allow test-fixture-address-of

package cohortsync

import (
	"net/http"
	"net/url"
	"testing"

	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

func TestBindListCohortsRequest(t *testing.T) {
	t.Run("with source_id", func(t *testing.T) {
		r := &http.Request{URL: &url.URL{RawQuery: "source_id=intercom"}}
		req := &attunev1.ListCohortsRequest{}

		if err := BindListCohortsRequest(r, req); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if req.SourceId == nil || *req.SourceId != "intercom" {
			t.Errorf("SourceId = %v, want pointer to %q", req.SourceId, "intercom")
		}
	})

	t.Run("without source_id", func(t *testing.T) {
		r := &http.Request{URL: &url.URL{RawQuery: ""}}
		req := &attunev1.ListCohortsRequest{}

		if err := BindListCohortsRequest(r, req); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if req.SourceId != nil {
			t.Errorf("SourceId = %v, want nil", req.SourceId)
		}
	})

	t.Run("whitespace-only source_id", func(t *testing.T) {
		r := &http.Request{URL: &url.URL{RawQuery: "source_id=++"}}
		req := &attunev1.ListCohortsRequest{}

		if err := BindListCohortsRequest(r, req); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// url-decoded "++" is "  " (two spaces), TrimSpace makes it empty.
		if req.SourceId != nil {
			t.Errorf("SourceId = %v, want nil for whitespace-only input", req.SourceId)
		}
	})
}

func TestBindListRunsRequest(t *testing.T) {
	t.Run("with limit", func(t *testing.T) {
		r := &http.Request{URL: &url.URL{RawQuery: "limit=25"}}
		req := &attunev1.ListCohortSyncRunsRequest{}

		if err := BindListRunsRequest(r, req); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if req.Limit == nil || *req.Limit != 25 {
			t.Errorf("Limit = %v, want pointer to 25", req.Limit)
		}
	})

	t.Run("without limit", func(t *testing.T) {
		r := &http.Request{URL: &url.URL{RawQuery: ""}}
		req := &attunev1.ListCohortSyncRunsRequest{}

		if err := BindListRunsRequest(r, req); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if req.Limit != nil {
			t.Errorf("Limit = %v, want nil", req.Limit)
		}
	})

	t.Run("invalid limit ignored", func(t *testing.T) {
		r := &http.Request{URL: &url.URL{RawQuery: "limit=abc"}}
		req := &attunev1.ListCohortSyncRunsRequest{}

		if err := BindListRunsRequest(r, req); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if req.Limit != nil {
			t.Errorf("Limit = %v, want nil for non-numeric input", req.Limit)
		}
	})

	t.Run("zero limit ignored", func(t *testing.T) {
		r := &http.Request{URL: &url.URL{RawQuery: "limit=0"}}
		req := &attunev1.ListCohortSyncRunsRequest{}

		if err := BindListRunsRequest(r, req); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if req.Limit != nil {
			t.Errorf("Limit = %v, want nil for zero limit", req.Limit)
		}
	})

	t.Run("negative limit ignored", func(t *testing.T) {
		r := &http.Request{URL: &url.URL{RawQuery: "limit=-5"}}
		req := &attunev1.ListCohortSyncRunsRequest{}

		if err := BindListRunsRequest(r, req); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if req.Limit != nil {
			t.Errorf("Limit = %v, want nil for negative limit", req.Limit)
		}
	})
}
