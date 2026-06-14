// SPDX-License-Identifier: Apache-2.0

// Package batchconv provides shared conversion utilities for batch operations.
package batchconv

import (
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/feedback"
)

// ItemFailuresToProto converts repo ItemFailures to proto BatchItemFailures.
func ItemFailuresToProto(failures []feedback.ItemFailure) []*attunev1.BatchItemFailure {
	if len(failures) == 0 {
		return nil
	}

	result := make([]*attunev1.BatchItemFailure, len(failures))
	for i, f := range failures {
		result[i] = ItemFailureToProto(f)
	}
	return result
}

// ItemFailureToProto converts a single repo ItemFailure to proto BatchItemFailure.
func ItemFailureToProto(f feedback.ItemFailure) *attunev1.BatchItemFailure {
	pf := ptrext.Of(attunev1.BatchItemFailure{
		FeedbackId: f.FeedbackID,
		Code:       f.Code,
		Message:    f.Message,
	})
	if f.UpdatedAt != nil {
		ts := f.UpdatedAt.Format(time.RFC3339)
		pf.CurrentUpdatedAt = ptrext.Of(ts)
	}
	return pf
}
