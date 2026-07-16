package feedback

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestQualityAccumulatorAdversarialSemanticInvariants(t *testing.T) {
	t.Parallel()

	opts, row, longValue := adversarialQualityFixture(t)
	acc := newQualityTestAccumulator()
	acc.consumeSemantic(opts, row)
	acc.consumeFailure(opts, adversarialFailureRow(row.EventAt))

	key := keyFromEvent(opts, row.EventAt, row.Source, row.LogicalModel, row.ProviderModel, row.ChannelID)
	requireAdversarialQualitySignal(t, acc.signals[key])
	requireAdversarialQualityValues(t, acc, opts, row, longValue)
	requireQualityAccumulatorInvariants(t, acc)
}

func adversarialQualityFixture(t *testing.T) (ClassificationQualityRefreshOpts, semanticQualityRow, string) {
	t.Helper()
	opts := normalizeQualityRefreshOpts(ClassificationQualityRefreshOpts{
		From:                   time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
		To:                     time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC),
		BucketWidth:            QualityBucketHour,
		LowConfidenceThreshold: 0.50,
	})
	longValue := strings.Repeat("界", 80)
	row := semanticQualityRow{
		ID:            17,
		EventAt:       time.Date(2026, 7, 2, 8, 45, 0, 0, time.UTC),
		FeedbackID:    101,
		Source:        " api ",
		LogicalModel:  " classifier ",
		ProviderModel: " provider ",
		ChannelID:     " primary ",
		Attrs: mustQualityJSON(t, map[string]any{
			"severity":       []string{" bug ", "bug", "", "praise"},
			"Bad Dimension!": "invalid-dim-value",
			"long_value":     longValue,
			"number":         42,
		}),
		DroppedAttrs: mustQualityJSON(t, droppedAttrsPayload{Diagnostics: []domain.AttrDropDiagnostic{
			{
				Dim:    "severity",
				Reason: domain.AttrDropOffListValue,
				Values: []string{" refund ", "refund", ""},
				Count:  2,
			},
			{
				Dim:    "Unknown Dimension!",
				Reason: domain.AttrDropUnknownDimension,
				Values: []string{"custom"},
				Count:  1,
			},
			{
				Dim:    "severity",
				Reason: domain.AttrDropOffListValue,
				Values: []string{"negative-count"},
				Count:  -7,
			},
		}}),
		Confidence: []byte(`{"overall":0.42}`),
	}
	return opts, row, longValue
}

func adversarialFailureRow(eventAt time.Time) failureQualityRow {
	return failureQualityRow{
		ID:            23,
		EventAt:       eventAt,
		FeedbackID:    202,
		Source:        " api ",
		LogicalModel:  " classifier ",
		ProviderModel: " provider ",
		ChannelID:     " primary ",
		ReasonClass:   "parse_err",
		Terminal:      true,
	}
}

func requireAdversarialQualitySignal(t *testing.T, signal *ClassificationQualitySignalAggregate) {
	t.Helper()
	require.NotNil(t, signal)
	require.Equal(t, int64(1), signal.ClassificationEventCount)
	require.Equal(t, int64(1), signal.FailedAttemptCount)
	require.Equal(t, int64(1), signal.ParseFailureCount)
	require.Equal(t, int64(1), signal.TerminalFailureCount)
	require.Equal(t, int64(1), signal.TerminalParseFailureCount)
	require.Equal(t, int64(2), signal.OffListCount)
	require.Equal(t, int64(1), signal.UnknownDimensionCount)
	require.Equal(t, int64(1), signal.ConfidenceCount)
	require.InDelta(t, 0.42, signal.ConfidenceSum, 0.001)
	require.Equal(t, int64(1), signal.LowConfidenceCount)
	require.Equal(t, []int64{101, 202}, signal.SampleFeedbackIDs)
	require.Equal(t, []int64{101}, signal.LowConfidenceSampleFeedbackIDs)
	require.Equal(t, []int64{101}, signal.OffListSampleFeedbackIDs)
	require.Equal(t, []int64{202}, signal.ParseFailureSampleFeedbackIDs)
	require.Equal(t, []int64{202}, signal.TerminalFailureSampleFeedbackIDs)
}

func requireAdversarialQualityValues(
	t *testing.T,
	acc *qualityAccumulator,
	opts ClassificationQualityRefreshOpts,
	row semanticQualityRow,
	longValue string,
) {
	t.Helper()
	_, bugHash := normalizeQualityValue("bug")
	bug := acc.values[valueKeyFromEvent(opts, row, "severity", bugHash, QualityValueConfigured)]
	require.NotNil(t, bug)
	require.Equal(t, int64(2), bug.AppearanceCount)
	require.Equal(t, int64(1), bug.EventCount)
	require.Equal(t, int64(1), bug.ConfidenceCount)
	require.Equal(t, int64(1), bug.LowConfidenceCount)

	severityAll := acc.values[valueKeyFromEvent(opts, row, "severity", "", QualityValueAll)]
	require.NotNil(t, severityAll)
	require.Equal(t, int64(1), severityAll.EventCount)
	require.Equal(t, []int64{101}, severityAll.SampleFeedbackIDs)

	_, invalidHash := normalizeQualityValue("invalid-dim-value")
	invalidDim := acc.values[valueKeyFromEvent(opts, row, InvalidDimensionName, invalidHash, QualityValueConfigured)]
	require.NotNil(t, invalidDim)
	require.Equal(t, InvalidDimensionName, invalidDim.DimensionName)

	longDisplay, longHash := normalizeQualityValue(longValue)
	longAgg := acc.values[valueKeyFromEvent(opts, row, "long_value", longHash, QualityValueConfigured)]
	require.NotNil(t, longAgg)
	require.Equal(t, longDisplay, longAgg.DimensionValueDisplay)
	require.True(t, utf8.ValidString(longAgg.DimensionValueDisplay))
	require.LessOrEqual(t, len(longAgg.DimensionValueDisplay), qualityValueDisplayCap)

	_, refundHash := normalizeQualityValue("refund")
	refund := acc.values[valueKeyFromEvent(opts, row, "severity", refundHash, QualityValueOffList)]
	require.NotNil(t, refund)
	require.Equal(t, int64(2), refund.AppearanceCount)
	require.Equal(t, int64(1), refund.EventCount)
	require.Equal(t, int64(1), refund.ConfidenceCount)
	require.Equal(t, int64(1), refund.LowConfidenceCount)

	_, negativeHash := normalizeQualityValue("negative-count")
	require.Nil(t, acc.values[valueKeyFromEvent(opts, row, "severity", negativeHash, QualityValueOffList)])
}

func FuzzNormalizeQualityValue(f *testing.F) {
	for _, seed := range []string{
		"",
		"   ",
		"bug",
		strings.Repeat("a", qualityValueDisplayCap+20),
		strings.Repeat("界", 80),
		"emoji 🧪 value",
		string([]byte{0xff, 0xfe, 'a'}),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		if len(value) > 4096 {
			t.Skip()
		}
		display, hash := normalizeQualityValue(value)
		if strings.TrimSpace(value) == "" {
			require.Empty(t, display)
			require.Empty(t, hash)
			return
		}
		require.Len(t, hash, 64)
		require.True(t, utf8.ValidString(display))
		require.LessOrEqual(t, len(display), qualityValueDisplayCap)
	})
}

func FuzzQualityAccumulatorMalformedPayloads(f *testing.F) {
	f.Add(
		`{"severity":["bug","bug"],"invalid dim":"x"}`,
		`{"diagnostics":[{"dim":"severity","reason":"off_list_value","values":["refund","refund"],"count":2}]}`,
		`{"overall":0.42}`,
	)
	f.Add(
		`{"severity":42}`,
		`{"diagnostics":[{"dim":"severity","reason":"off_list_value","values":["negative"],"count":-4}]}`,
		`{"overall":1.4}`,
	)
	f.Add(`not-json`, `{"diagnostics":"wrong-shape"}`, `not-json`)
	f.Fuzz(func(t *testing.T, attrs string, dropped string, confidence string) {
		if len(attrs)+len(dropped)+len(confidence) > 8192 {
			t.Skip()
		}
		opts := normalizeQualityRefreshOpts(ClassificationQualityRefreshOpts{
			BucketWidth:            QualityBucketHour,
			LowConfidenceThreshold: 0.50,
		})
		acc := newQualityTestAccumulator()
		row := semanticQualityRow{
			ID:            1,
			EventAt:       time.Date(2026, 7, 2, 9, 0, 0, 0, time.UTC),
			FeedbackID:    101,
			Source:        "api",
			LogicalModel:  "classifier",
			ProviderModel: "provider",
			ChannelID:     "primary",
			Attrs:         []byte(attrs),
			DroppedAttrs:  []byte(dropped),
			Confidence:    []byte(confidence),
		}
		require.NotPanics(t, func() {
			acc.consumeSemantic(opts, row)
			acc.consumeFailure(opts, failureQualityRow{
				ID:          1,
				EventAt:     row.EventAt,
				FeedbackID:  -1,
				ReasonClass: "parse_err",
				Terminal:    true,
			})
		})
		requireQualityAccumulatorInvariants(t, acc)
	})
}

func TestQualityBucketRangeCoversPartialDayWindow(t *testing.T) {
	t.Parallel()

	from := time.Date(2026, 7, 2, 1, 30, 0, 0, time.UTC)
	to := time.Date(2026, 7, 2, 3, 0, 0, 0, time.UTC)

	gotFrom, gotTo := qualityBucketRange(from, to, QualityBucketDay)

	require.Equal(t, time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC), gotFrom)
	require.Equal(t, time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC), gotTo)
}

func TestQualityBucketRangeKeepsExactHourExclusiveEnd(t *testing.T) {
	t.Parallel()

	from := time.Date(2026, 7, 2, 1, 30, 0, 0, time.UTC)
	to := time.Date(2026, 7, 2, 3, 0, 0, 0, time.UTC)

	gotFrom, gotTo := qualityBucketRange(from, to, QualityBucketHour)

	require.Equal(t, time.Date(2026, 7, 2, 1, 0, 0, 0, time.UTC), gotFrom)
	require.Equal(t, time.Date(2026, 7, 2, 3, 0, 0, 0, time.UTC), gotTo)
}

func TestQualityNormalizationHelpersCoverDefaultsAndInvalidValues(t *testing.T) {
	t.Parallel()

	from := time.Date(2026, 7, 2, 9, 15, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	to := from.Add(2*time.Hour + 30*time.Minute)

	refresh := normalizeQualityRefreshOpts(ClassificationQualityRefreshOpts{
		From:                   from,
		To:                     to,
		BucketWidth:            "week",
		LowConfidenceThreshold: 1.2,
	})
	require.Equal(t, QualityBucketDay, refresh.BucketWidth)
	require.Equal(t, qualityDefaultThreshold, refresh.LowConfidenceThreshold)
	require.Equal(t, from.UTC(), refresh.From)
	require.Equal(t, to.UTC(), refresh.To)

	query := normalizeQualityQueryOpts(ClassificationQualityQueryOpts{
		From:        from,
		To:          to,
		BucketWidth: "week",
	})
	require.Equal(t, QualityBucketDay, query.BucketWidth)
	require.Equal(t, time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC), query.From)
	require.Equal(t, time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC), query.To)
}

func TestQualityScalarHelpersCoverEdgeCases(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{"bug"}, attrValues(" bug "))
	require.Equal(t, []string{"bug", "praise"}, attrValues([]any{" bug ", 42, "", "praise"}))
	require.Equal(t, []string{"bug", "praise"}, attrValues([]string{" bug ", "", "praise"}))
	require.Nil(t, attrValues(42))

	for _, raw := range [][]byte{
		nil,
		[]byte(`not-json`),
		[]byte(`{"overall":"high"}`),
		[]byte(`{"overall":-0.1}`),
		[]byte(`{"overall":1.1}`),
	} {
		_, ok := confidenceFromAudit(raw)
		require.False(t, ok)
	}
	got, ok := confidenceFromAudit([]byte(`{"overall":1}`))
	require.True(t, ok)
	require.Equal(t, 1.0, got)

	require.Empty(t, qualitySamplesForSQL(nil))
	require.Equal(t, []int64{1, 2}, qualitySamplesForSQL([]int64{1, 2}))

	agg := ptrext.Of(ClassificationQualityValueAggregate{})
	addValueConfidence(agg, 0.2, false, 0.5)
	require.Zero(t, agg.ConfidenceCount)
	require.Zero(t, agg.LowConfidenceCount)
}

func TestQualityMergeHelpersDeduplicateAndCapSamples(t *testing.T) {
	t.Parallel()

	var signal ClassificationQualitySignalAggregate
	signal.mergeSignal(ClassificationQualitySignalAggregate{
		ClassificationEventCount:         1,
		FailedAttemptCount:               2,
		ParseFailureCount:                3,
		TerminalFailureCount:             4,
		TerminalParseFailureCount:        5,
		OffListCount:                     6,
		UnknownDimensionCount:            7,
		ConfidenceCount:                  8,
		ConfidenceSum:                    3.5,
		LowConfidenceCount:               9,
		SampleFeedbackIDs:                []int64{1, 2, 2, -1},
		LowConfidenceSampleFeedbackIDs:   []int64{10, 11, 12, 13, 14, 15},
		OffListSampleFeedbackIDs:         []int64{20, 21},
		ParseFailureSampleFeedbackIDs:    []int64{30},
		TerminalFailureSampleFeedbackIDs: []int64{40},
	})
	signal.mergeSignal(ClassificationQualitySignalAggregate{
		ClassificationEventCount:         10,
		ConfidenceSum:                    1.5,
		SampleFeedbackIDs:                []int64{2, 3},
		LowConfidenceSampleFeedbackIDs:   []int64{15, 16},
		OffListSampleFeedbackIDs:         []int64{21, 22},
		ParseFailureSampleFeedbackIDs:    []int64{30, 31},
		TerminalFailureSampleFeedbackIDs: []int64{40, 41},
	})

	require.Equal(t, int64(11), signal.ClassificationEventCount)
	require.Equal(t, int64(2), signal.FailedAttemptCount)
	require.Equal(t, int64(3), signal.ParseFailureCount)
	require.Equal(t, int64(4), signal.TerminalFailureCount)
	require.Equal(t, int64(5), signal.TerminalParseFailureCount)
	require.Equal(t, int64(6), signal.OffListCount)
	require.Equal(t, int64(7), signal.UnknownDimensionCount)
	require.Equal(t, int64(8), signal.ConfidenceCount)
	require.InDelta(t, 5.0, signal.ConfidenceSum, 0.001)
	require.Equal(t, int64(9), signal.LowConfidenceCount)
	require.Equal(t, []int64{1, 2, 3}, signal.SampleFeedbackIDs)
	require.Equal(t, []int64{10, 11, 12, 13, 14}, signal.LowConfidenceSampleFeedbackIDs)
	require.Equal(t, []int64{20, 21, 22}, signal.OffListSampleFeedbackIDs)
	require.Equal(t, []int64{30, 31}, signal.ParseFailureSampleFeedbackIDs)
	require.Equal(t, []int64{40, 41}, signal.TerminalFailureSampleFeedbackIDs)

	merged := map[string]*ClassificationQualityValueAggregate{}
	row := ClassificationQualityValueAggregate{
		DimensionName:         "kind",
		DimensionValueHash:    "hash",
		DimensionValueDisplay: "Bug",
		ValueStatus:           QualityValueConfigured,
		AppearanceCount:       2,
		EventCount:            1,
		ConfidenceCount:       1,
		ConfidenceSum:         0.4,
		LowConfidenceCount:    1,
		SampleFeedbackIDs:     []int64{1, 2, 2},
	}
	mergeQualityValue(merged, row)
	row.AppearanceCount = 3
	row.EventCount = 2
	row.ConfidenceCount = 2
	row.ConfidenceSum = 1.2
	row.LowConfidenceCount = 0
	row.SampleFeedbackIDs = []int64{2, 3, 4, 5, 6}
	mergeQualityValue(merged, row)
	mergeQualityValue(merged, ClassificationQualityValueAggregate{
		DimensionName:      "kind",
		DimensionValueHash: "other",
		ValueStatus:        QualityValueOffList,
		AppearanceCount:    1,
	})

	require.Len(t, merged, 2)
	gotValue := merged["kind\x00hash\x00"+QualityValueConfigured]
	require.NotNil(t, gotValue)
	require.Equal(t, "Bug", gotValue.DimensionValueDisplay)
	require.Equal(t, int64(5), gotValue.AppearanceCount)
	require.Equal(t, int64(3), gotValue.EventCount)
	require.Equal(t, int64(3), gotValue.ConfidenceCount)
	require.InDelta(t, 1.6, gotValue.ConfidenceSum, 0.001)
	require.Equal(t, int64(1), gotValue.LowConfidenceCount)
	require.Equal(t, []int64{1, 2, 3, 4, 5}, gotValue.SampleFeedbackIDs)
}

func TestQualityBucketWriteHelpersUseTransaction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	opts := normalizeQualityRefreshOpts(ClassificationQualityRefreshOpts{
		TenantID:    "tenant-1",
		From:        time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
		To:          time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC),
		BucketWidth: QualityBucketHour,
	})
	key := qualitySignalKey{
		BucketStart:   opts.From,
		BucketWidth:   opts.BucketWidth,
		Source:        "api",
		LogicalModel:  "classifier",
		ProviderModel: "provider",
		ChannelID:     "primary",
	}
	valueKey := qualityValueKey{
		qualitySignalKey:   key,
		DimensionName:      "severity",
		DimensionValueHash: "hash-1",
		ValueStatus:        QualityValueConfigured,
	}
	acc := qualityAccumulator{
		signals: map[qualitySignalKey]*ClassificationQualitySignalAggregate{
			key: {
				ClassificationEventCount:         2,
				FailedAttemptCount:               1,
				ParseFailureCount:                1,
				TerminalFailureCount:             1,
				TerminalParseFailureCount:        1,
				OffListCount:                     1,
				UnknownDimensionCount:            1,
				ConfidenceCount:                  2,
				ConfidenceSum:                    1.1,
				LowConfidenceCount:               1,
				SampleFeedbackIDs:                []int64{101},
				LowConfidenceSampleFeedbackIDs:   []int64{101},
				OffListSampleFeedbackIDs:         []int64{101},
				ParseFailureSampleFeedbackIDs:    []int64{202},
				TerminalFailureSampleFeedbackIDs: []int64{202},
			},
		},
		values: map[qualityValueKey]*ClassificationQualityValueAggregate{
			valueKey: {
				DimensionName:         "severity",
				DimensionValueHash:    "hash-1",
				DimensionValueDisplay: "bug",
				ValueStatus:           QualityValueConfigured,
				AppearanceCount:       2,
				EventCount:            1,
				ConfidenceCount:       1,
				ConfidenceSum:         0.4,
				LowConfidenceCount:    1,
				SampleFeedbackIDs:     []int64{101},
			},
		},
		maxRunID:     17,
		maxFailureID: 23,
	}
	tx := ptrext.Of(fakeFeedbackBatchTx{})

	require.NoError(t, lockQualityRefresh(ctx, tx, opts))
	require.NoError(t, deleteQualityBuckets(ctx, tx, opts))
	require.NoError(t, insertQualitySignals(ctx, tx, opts, acc.signals))
	require.NoError(t, insertQualityValues(ctx, tx, opts, acc.values))
	require.NoError(t, upsertQualityState(ctx, tx, opts, acc))
	require.Equal(t, 6, tx.execIdx)
}

func TestQualityBucketWriteHelpersReturnExecErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	opts := normalizeQualityRefreshOpts(ClassificationQualityRefreshOpts{
		TenantID:    "tenant-1",
		From:        time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
		To:          time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC),
		BucketWidth: QualityBucketDay,
	})
	boom := errors.New("boom")
	signalKey := qualitySignalKey{BucketStart: opts.From, BucketWidth: opts.BucketWidth}
	valueKey := qualityValueKey{
		qualitySignalKey:   signalKey,
		DimensionName:      "severity",
		DimensionValueHash: "hash-1",
		ValueStatus:        QualityValueConfigured,
	}

	tests := []struct {
		name string
		call func() error
		want string
	}{
		{
			name: "lock",
			call: func() error {
				return lockQualityRefresh(ctx, ptrext.Of(fakeFeedbackBatchTx{execErrs: []error{boom}}), opts)
			},
			want: "lock quality refresh",
		},
		{
			name: "delete",
			call: func() error {
				return deleteQualityBuckets(ctx, ptrext.Of(fakeFeedbackBatchTx{execErrs: []error{nil, boom}}), opts)
			},
			want: "delete classification_quality_signal_buckets",
		},
		{
			name: "insert signals",
			call: func() error {
				return insertQualitySignals(ctx, ptrext.Of(fakeFeedbackBatchTx{execErrs: []error{boom}}), opts, map[qualitySignalKey]*ClassificationQualitySignalAggregate{
					signalKey: {},
				})
			},
			want: "insert quality signal bucket",
		},
		{
			name: "insert values",
			call: func() error {
				return insertQualityValues(ctx, ptrext.Of(fakeFeedbackBatchTx{execErrs: []error{boom}}), opts, map[qualityValueKey]*ClassificationQualityValueAggregate{
					valueKey: {DimensionName: "severity", DimensionValueHash: "hash-1", ValueStatus: QualityValueConfigured},
				})
			},
			want: "insert quality value bucket",
		},
		{
			name: "upsert state",
			call: func() error {
				return upsertQualityState(ctx, ptrext.Of(fakeFeedbackBatchTx{execErrs: []error{boom}}), opts, qualityAccumulator{})
			},
			want: "upsert quality state",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.call()
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

func newQualityTestAccumulator() *qualityAccumulator {
	return ptrext.Of(qualityAccumulator{
		signals: make(map[qualitySignalKey]*ClassificationQualitySignalAggregate),
		values:  make(map[qualityValueKey]*ClassificationQualityValueAggregate),
	})
}

func mustQualityJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	return raw
}

func requireQualityAccumulatorInvariants(t *testing.T, acc *qualityAccumulator) {
	t.Helper()
	for _, signal := range acc.signals {
		require.GreaterOrEqual(t, signal.ClassificationEventCount, int64(0))
		require.GreaterOrEqual(t, signal.FailedAttemptCount, int64(0))
		require.GreaterOrEqual(t, signal.ParseFailureCount, int64(0))
		require.GreaterOrEqual(t, signal.TerminalFailureCount, int64(0))
		require.GreaterOrEqual(t, signal.TerminalParseFailureCount, int64(0))
		require.GreaterOrEqual(t, signal.OffListCount, int64(0))
		require.GreaterOrEqual(t, signal.UnknownDimensionCount, int64(0))
		require.GreaterOrEqual(t, signal.ConfidenceCount, int64(0))
		require.GreaterOrEqual(t, signal.ConfidenceSum, 0.0)
		require.GreaterOrEqual(t, signal.LowConfidenceCount, int64(0))
		requireQualitySamples(t, signal.SampleFeedbackIDs)
		requireQualitySamples(t, signal.LowConfidenceSampleFeedbackIDs)
		requireQualitySamples(t, signal.OffListSampleFeedbackIDs)
		requireQualitySamples(t, signal.ParseFailureSampleFeedbackIDs)
		requireQualitySamples(t, signal.TerminalFailureSampleFeedbackIDs)
	}
	for _, value := range acc.values {
		require.GreaterOrEqual(t, value.AppearanceCount, int64(0))
		require.GreaterOrEqual(t, value.EventCount, int64(0))
		require.GreaterOrEqual(t, value.ConfidenceCount, int64(0))
		require.GreaterOrEqual(t, value.ConfidenceSum, 0.0)
		require.GreaterOrEqual(t, value.LowConfidenceCount, int64(0))
		require.True(t, utf8.ValidString(value.DimensionValueDisplay))
		require.LessOrEqual(t, len(value.DimensionValueDisplay), qualityValueDisplayCap)
		if value.ValueStatus != QualityValueAll {
			require.LessOrEqual(t, value.EventCount, value.AppearanceCount)
		}
		requireQualitySamples(t, value.SampleFeedbackIDs)
	}
}

func requireQualitySamples(t *testing.T, samples []int64) {
	t.Helper()
	require.LessOrEqual(t, len(samples), qualitySampleCap)
	seen := map[int64]bool{}
	for _, id := range samples {
		require.Positive(t, id)
		require.False(t, seen[id])
		seen[id] = true
	}
}
