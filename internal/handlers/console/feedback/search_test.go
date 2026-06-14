// SPDX-License-Identifier: Apache-2.0

package feedback

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	repofeedback "github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/service/semanticsearch"
)

func TestProtoFilterToRepoFilter_NilInput(t *testing.T) {
	t.Parallel()
	result := protoFilterToRepoFilter(nil)
	assert.Nil(t, result)
}

func TestProtoFilterToRepoFilter_EmptyFilter(t *testing.T) {
	t.Parallel()
	pf := ptrext.Of(attunev1.FeedbackFilter{})
	result := protoFilterToRepoFilter(pf)

	require.NotNil(t, result)
	assert.Empty(t, result.Attrs)
	assert.Nil(t, result.Urgent)
	assert.Empty(t, result.TagIDs)
	assert.Empty(t, result.WorkflowStateIDs)
	assert.Nil(t, result.WorkflowCategory)
}

func TestProtoFilterToRepoFilter_FullFilter(t *testing.T) {
	t.Parallel()
	pf := ptrext.Of(attunev1.FeedbackFilter{
		Attrs: []*attunev1.AttrFilter{
			{Dim: "severity", Value: "high"},
			{Dim: "labels", Value: "bug"},
		},
		Urgent:           ptrext.Of(true),
		Q:                ptrext.Of("search query"),
		TagId:            ptrext.Of("tag-123"),
		WorkflowStateId:  ptrext.Of("state-456"),
		WorkflowCategory: ptrext.Of("open"),
	})
	result := protoFilterToRepoFilter(pf)

	require.NotNil(t, result)
	assert.Len(t, result.Attrs, 2)
	assert.Equal(t, "severity", result.Attrs[0].Dim)
	assert.Equal(t, "high", result.Attrs[0].Value)
	assert.Equal(t, "labels", result.Attrs[1].Dim)
	assert.Equal(t, "bug", result.Attrs[1].Value)
	require.NotNil(t, result.Urgent)
	assert.True(t, ptrext.Indirect(result.Urgent))
	assert.Equal(t, "search query", result.Q)
	assert.Equal(t, []string{"tag-123"}, result.TagIDs)
	assert.Equal(t, []string{"state-456"}, result.WorkflowStateIDs)
	require.NotNil(t, result.WorkflowCategory)
	assert.Equal(t, "open", ptrext.Indirect(result.WorkflowCategory))
}

func TestProtoFilterToRepoFilter_SkipsEmptyAttrs(t *testing.T) {
	t.Parallel()
	pf := ptrext.Of(attunev1.FeedbackFilter{
		Attrs: []*attunev1.AttrFilter{
			{Dim: "", Value: "value"},      // empty dim
			{Dim: "dim", Value: ""},        // empty value
			nil,                            // nil attr
			{Dim: "valid", Value: "value"}, // valid
		},
	})
	result := protoFilterToRepoFilter(pf)

	require.NotNil(t, result)
	assert.Len(t, result.Attrs, 1)
	assert.Equal(t, "valid", result.Attrs[0].Dim)
}

func TestServiceResponseToProto_NilInput(t *testing.T) {
	t.Parallel()
	result := serviceResponseToProto(nil)
	require.NotNil(t, result)
	assert.Empty(t, result.GetHits())
	assert.Empty(t, result.GetEmbeddingModel())
	assert.Zero(t, result.GetTotalWithEmbeddings())
	assert.False(t, result.GetUsedKeywordFallback())
}

func TestServiceResponseToProto_EmptyResponse(t *testing.T) {
	t.Parallel()
	resp := ptrext.Of(semanticsearch.SearchResponse{})
	result := serviceResponseToProto(resp)

	require.NotNil(t, result)
	assert.Empty(t, result.GetHits())
}

func TestServiceResponseToProto_WithHits(t *testing.T) {
	t.Parallel()
	now := time.Now()
	resp := ptrext.Of(semanticsearch.SearchResponse{
		Hits: []*semanticsearch.SearchHit{
			{
				Feedback: ptrext.Of(repofeedback.SearchFeedback{
					ID:               1,
					Content:          "test content",
					Source:           "widget",
					EnrichedTitle:    "Test Title",
					IsUrgent:         true,
					EnrichmentStatus: "done",
					CreatedAt:        now,
				}),
				Similarity:   0.95,
				KeywordScore: 0.8,
				MatchType:    "hybrid",
			},
			nil, // should be skipped
			{
				Feedback: nil, // should be skipped
			},
		},
		EmbeddingModel:      "text-embedding-3-small",
		TotalWithEmbeddings: 100,
		UsedKeywordFallback: false,
	})
	result := serviceResponseToProto(resp)

	require.NotNil(t, result)
	assert.Len(t, result.GetHits(), 1)

	hit := result.GetHits()[0]
	assert.Equal(t, int64(1), hit.GetFeedback().GetId())
	assert.Equal(t, "test content", hit.GetFeedback().GetContent())
	assert.Equal(t, "widget", hit.GetFeedback().GetSource())
	assert.Equal(t, "Test Title", hit.GetFeedback().GetEnrichedTitle())
	assert.True(t, hit.GetFeedback().GetIsUrgent())
	assert.Equal(t, "done", hit.GetFeedback().GetEnrichmentStatus())
	assert.InDelta(t, float32(0.95), hit.GetSimilarity(), 0.001)
	assert.InDelta(t, float32(0.8), hit.GetKeywordScore(), 0.001)
	assert.Equal(t, "hybrid", hit.GetMatchType())

	assert.Equal(t, "text-embedding-3-small", result.GetEmbeddingModel())
	assert.Equal(t, int32(100), result.GetTotalWithEmbeddings())
	assert.False(t, result.GetUsedKeywordFallback())
}

func TestServiceResponseToProto_KeywordFallback(t *testing.T) {
	t.Parallel()
	resp := ptrext.Of(semanticsearch.SearchResponse{
		Hits: []*semanticsearch.SearchHit{
			{
				Feedback: ptrext.Of(repofeedback.SearchFeedback{
					ID:      1,
					Content: "test",
				}),
				KeywordScore: 1.0,
				MatchType:    "keyword",
			},
		},
		UsedKeywordFallback: true,
	})
	result := serviceResponseToProto(resp)

	require.NotNil(t, result)
	assert.True(t, result.GetUsedKeywordFallback())
	assert.Len(t, result.GetHits(), 1)
	assert.Equal(t, "keyword", result.GetHits()[0].GetMatchType())
}

func TestSearchFeedbackToProto_NilInput(t *testing.T) {
	t.Parallel()
	result := searchFeedbackToProto(nil)
	assert.Nil(t, result)
}

func TestSearchFeedbackToProto_EmptyStrings(t *testing.T) {
	t.Parallel()
	fb := ptrext.Of(repofeedback.SearchFeedback{
		ID:               1,
		Content:          "content",
		Source:           "api",
		EnrichedTitle:    "",
		EnrichmentStatus: "pending",
		CreatedAt:        time.Now(),
	})
	result := searchFeedbackToProto(fb)

	require.NotNil(t, result)
	assert.Equal(t, int64(1), result.GetId())
	assert.Nil(t, result.EnrichedTitle) // empty string becomes nil
}
