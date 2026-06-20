package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	evalsvc "github.com/Phixsura/attune/internal/service/eval"
)

func TestEvalReportToEnrichconfig_MapsAllFields(t *testing.T) {
	t.Parallel()

	in := evalsvc.SuggestedAttrsReport{
		Coverage: map[string]float64{"modules": 0.85, "sentiment": 1.0},
		Candidates: []evalsvc.SuggestedCandidate{
			{Dim: "modules", Value: "checkout", Count: 12, Confidence: 0.24, CoverageImpact: 0.12},
			{Dim: "modules", Value: "billing", Count: 5, Confidence: 0.10, CoverageImpact: 0.05},
		},
		Recommendations: []evalsvc.SuggestedRecommendation{
			{Action: "add", Dim: "modules", Value: "checkout", Reason: "appeared 12 times", Impact: "+12% coverage"},
		},
	}

	out := evalReportToEnrichconfig(in)
	require.NotNil(t, out)

	require.Equal(t, in.Coverage, out.Coverage)

	require.Len(t, out.Candidates, 2)
	require.Equal(t, "modules", out.Candidates[0].Dim)
	require.Equal(t, "checkout", out.Candidates[0].Value)
	require.Equal(t, 12, out.Candidates[0].Count)
	require.InDelta(t, 0.24, out.Candidates[0].Confidence, 1e-9)
	require.InDelta(t, 0.12, out.Candidates[0].CoverageImpact, 1e-9)
	require.Equal(t, "billing", out.Candidates[1].Value)

	require.Len(t, out.Recommendations, 1)
	require.Equal(t, "add", out.Recommendations[0].Action)
	require.Equal(t, "modules", out.Recommendations[0].Dim)
	require.Equal(t, "checkout", out.Recommendations[0].Value)
	require.Equal(t, "appeared 12 times", out.Recommendations[0].Reason)
	require.Equal(t, "+12% coverage", out.Recommendations[0].Impact)
}

func TestEvalReportToEnrichconfig_EmptyReport(t *testing.T) {
	t.Parallel()

	out := evalReportToEnrichconfig(evalsvc.SuggestedAttrsReport{})
	require.NotNil(t, out)
	require.Empty(t, out.Coverage)
	require.Empty(t, out.Candidates)
	require.Empty(t, out.Recommendations)
}
