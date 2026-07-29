// SPDX-License-Identifier: Apache-2.0

package cohortsync

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// BindListCohortsRequest parses query params for ListCohorts.
func BindListCohortsRequest(r *http.Request, req *attunev1.ListCohortsRequest) error {
	if sid := strings.TrimSpace(r.URL.Query().Get("source_id")); sid != "" {
		req.SourceId = ptrext.Of(sid)
	}
	return nil
}

// BindListRunsRequest parses query params for ListCohortSyncRuns.
func BindListRunsRequest(r *http.Request, req *attunev1.ListCohortSyncRunsRequest) error {
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 32)
		if err == nil && n > 0 {
			req.Limit = ptrext.Of(int32(n))
		}
	}
	return nil
}
