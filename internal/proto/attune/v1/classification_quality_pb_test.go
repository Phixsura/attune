package attunev1

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/pkg/ptrext"

	"google.golang.org/protobuf/reflect/protoreflect"
)

const (
	qualityTestEnrichedAt   = "2026-07-02T00:01:00Z"
	qualityTestDisplayTitle = "Checkout fails"
	qualityTestConfidence   = 0.42
)

type generatedClassificationQualityMessage interface {
	Reset()
	String() string
	ProtoReflect() protoreflect.Message
	Descriptor() ([]byte, []int)
}

type classificationQualityProtoFixture struct {
	req         *GetClassificationQualityRequest
	samplesReq  *GetClassificationQualitySamplesRequest
	resp        *GetClassificationQualityResponse
	samplesResp *GetClassificationQualitySamplesResponse
	summary     *ClassificationQualitySummary
	bucket      *ClassificationQualityTimeBucket
	dimension   *ClassificationDimensionDrift
	value       *ClassificationValueDrift
	warning     *ClassificationQualityWarning
	sample      *ClassificationQualitySample
}

func TestClassificationQualityGeneratedAccessors(t *testing.T) {
	fixture := newClassificationQualityProtoFixture()

	requireEqual(t, fixture.req.GetBucketWidth(), "day")
	requireEqual(t, len(fixture.samplesReq.GetIds()), 2)
	requireEqual(t, fixture.resp.GetSummary().GetWorstSeverity(), "alert")
	requireEqual(t, fixture.resp.GetSeries()[0].GetBucket(), fixture.bucket.Bucket)
	requireEqual(t, fixture.resp.GetDimensions()[0].GetValues()[0].GetValueDisplay(), "bug")
	requireEqual(t, fixture.resp.GetWarnings()[0].GetMessage(), fixture.warning.Message)
	requireEqual(t, fixture.resp.GetSamples()[0].GetDisplayTitle(), qualityTestDisplayTitle)
	requireEqual(t, fixture.sample.GetEnrichedAt(), qualityTestEnrichedAt)
	requireEqual(t, fixture.sample.GetClassificationConfidence(), qualityTestConfidence)
	requireEqual(t, len(fixture.samplesResp.GetSamples()), 1)

	for _, tc := range fixture.messages() {
		callGeneratedGetters(t, tc.msg)
		touchGeneratedMessage(t, tc.msg, tc.name)
	}
	callGeneratedNilGetters(t)
}

func newClassificationQualityProtoFixture() classificationQualityProtoFixture {
	value := newClassificationValueDrift()
	summary := newClassificationQualitySummary()
	bucket := newClassificationQualityTimeBucket()
	dimension := newClassificationDimensionDrift(value)
	warning := newClassificationQualityWarning()
	sample := newClassificationQualitySample()
	req := newGetClassificationQualityRequest()
	return classificationQualityProtoFixture{
		req:         req,
		samplesReq:  ptrext.Of(GetClassificationQualitySamplesRequest{Ids: []int64{101, 102}}),
		resp:        newGetClassificationQualityResponse(req, summary, bucket, dimension, warning, sample),
		samplesResp: ptrext.Of(GetClassificationQualitySamplesResponse{Samples: []*ClassificationQualitySample{sample}}),
		summary:     summary,
		bucket:      bucket,
		dimension:   dimension,
		value:       value,
		warning:     warning,
		sample:      sample,
	}
}

func newClassificationValueDrift() *ClassificationValueDrift {
	return ptrext.Of(ClassificationValueDrift{
		ValueHash:         "sha256:value",
		ValueDisplay:      "bug",
		ValueStatus:       "configured",
		CurrentCount:      70,
		BaselineCount:     40,
		CurrentShare:      0.7,
		BaselineShare:     0.4,
		ShareDeltaPp:      30,
		Contribution:      0.3,
		SampleFeedbackIds: []int64{101},
	})
}

func newClassificationQualitySummary() *ClassificationQualitySummary {
	return ptrext.Of(ClassificationQualitySummary{
		ClassificationEvents: 100,
		FailedAttempts:       6,
		AverageConfidence:    0.73,
		LowConfidenceRate:    0.12,
		OffListRate:          0.06,
		UnknownDimensionRate: 0.01,
		ParseFailureRate:     0.03,
		TerminalFailureRate:  0.02,
		WorstSeverity:        "alert",
	})
}

func newClassificationQualityTimeBucket() *ClassificationQualityTimeBucket {
	return ptrext.Of(ClassificationQualityTimeBucket{
		Bucket:               "2026-07-01T00:00:00Z",
		ClassificationEvents: 100,
		FailedAttempts:       6,
		AverageConfidence:    0.73,
		LowConfidenceRate:    0.12,
		OffListRate:          0.06,
		UnknownDimensionRate: 0.01,
		ParseFailureRate:     0.03,
		TerminalFailureRate:  0.02,
	})
}

func newClassificationDimensionDrift(
	value *ClassificationValueDrift,
) *ClassificationDimensionDrift {
	return ptrext.Of(ClassificationDimensionDrift{
		DimensionName:        "severity",
		Severity:             "alert",
		Status:               "normal",
		CurrentCount:         100,
		BaselineCount:        100,
		JsDistance:           0.24,
		Psi:                  0.3,
		LowConfidenceRate:    0.12,
		OffListRate:          0.06,
		UnknownDimensionRate: 0.01,
		Values:               []*ClassificationValueDrift{value},
		SampleFeedbackIds:    []int64{101},
	})
}

func newClassificationQualityWarning() *ClassificationQualityWarning {
	return ptrext.Of(ClassificationQualityWarning{
		Reason:            "low_confidence_rate_spike",
		Severity:          "alert",
		DimensionName:     "severity",
		ValueDisplay:      "bug",
		Value:             0.12,
		Threshold:         0.1,
		Message:           "Quality signal crossed threshold",
		SampleFeedbackIds: []int64{101},
	})
}

func newClassificationQualitySample() *ClassificationQualitySample {
	return ptrext.Of(ClassificationQualitySample{
		Id:                       101,
		CreatedAt:                "2026-07-01T00:00:00Z",
		EnrichedAt:               ptrext.Of(qualityTestEnrichedAt),
		Source:                   "api",
		Title:                    "checkout broken",
		DisplayTitle:             ptrext.Of(qualityTestDisplayTitle),
		EnrichmentStatus:         "done",
		ClassificationConfidence: ptrext.Of(qualityTestConfidence),
		SignalReason:             "low_confidence_rate_spike",
	})
}

func newGetClassificationQualityRequest() *GetClassificationQualityRequest {
	return ptrext.Of(GetClassificationQualityRequest{
		CurrentFrom:            "2026-06-25T00:00:00Z",
		CurrentTo:              "2026-07-02T00:00:00Z",
		BaselineFrom:           "2026-05-28T00:00:00Z",
		BaselineTo:             "2026-06-25T00:00:00Z",
		BucketWidth:            "day",
		Source:                 "api",
		LogicalModel:           "gpt-4.1-mini",
		ProviderModel:          "gpt-4.1-mini-2026-04-14",
		ChannelId:              "primary",
		DimensionName:          "severity",
		Severity:               "alert",
		LowConfidenceThreshold: 0.5,
		Limit:                  20,
	})
}

func newGetClassificationQualityResponse(
	req *GetClassificationQualityRequest,
	summary *ClassificationQualitySummary,
	bucket *ClassificationQualityTimeBucket,
	dimension *ClassificationDimensionDrift,
	warning *ClassificationQualityWarning,
	sample *ClassificationQualitySample,
) *GetClassificationQualityResponse {
	return ptrext.Of(GetClassificationQualityResponse{
		GeneratedAt:      "2026-07-02T00:00:00Z",
		DataThrough:      "2026-07-02T00:00:00Z",
		RollupLagSeconds: 30,
		CurrentFrom:      req.CurrentFrom,
		CurrentTo:        req.CurrentTo,
		BaselineFrom:     req.BaselineFrom,
		BaselineTo:       req.BaselineTo,
		BucketWidth:      req.BucketWidth,
		Summary:          summary,
		Series:           []*ClassificationQualityTimeBucket{bucket},
		Dimensions:       []*ClassificationDimensionDrift{dimension},
		Warnings:         []*ClassificationQualityWarning{warning},
		Samples:          []*ClassificationQualitySample{sample},
	})
}

func (f classificationQualityProtoFixture) messages() []struct {
	msg  generatedClassificationQualityMessage
	name protoreflect.FullName
} {
	return []struct {
		msg  generatedClassificationQualityMessage
		name protoreflect.FullName
	}{
		{f.req, "attune.v1.GetClassificationQualityRequest"},
		{f.samplesReq, "attune.v1.GetClassificationQualitySamplesRequest"},
		{f.resp, "attune.v1.GetClassificationQualityResponse"},
		{f.samplesResp, "attune.v1.GetClassificationQualitySamplesResponse"},
		{f.summary, "attune.v1.ClassificationQualitySummary"},
		{f.bucket, "attune.v1.ClassificationQualityTimeBucket"},
		{f.dimension, "attune.v1.ClassificationDimensionDrift"},
		{f.value, "attune.v1.ClassificationValueDrift"},
		{f.warning, "attune.v1.ClassificationQualityWarning"},
		{f.sample, "attune.v1.ClassificationQualitySample"},
	}
}

func touchGeneratedMessage(
	t *testing.T,
	msg generatedClassificationQualityMessage,
	want protoreflect.FullName,
) {
	t.Helper()
	if msg.String() == "" {
		t.Fatalf("%T String() returned empty text", msg)
	}
	if got := msg.ProtoReflect().Descriptor().FullName(); got != want {
		t.Fatalf("%T descriptor name = %s, want %s", msg, got, want)
	}
	if raw, path := msg.Descriptor(); len(raw) == 0 || len(path) == 0 {
		t.Fatalf("%T Descriptor() returned empty raw=%d path=%d", msg, len(raw), len(path))
	}
	msg.Reset()
	if got := msg.ProtoReflect().Descriptor().FullName(); got != want {
		t.Fatalf("%T descriptor after Reset = %s, want %s", msg, got, want)
	}
}

func callGeneratedGetters(t *testing.T, msg any) {
	t.Helper()
	value := reflect.ValueOf(msg)
	typ := value.Type()
	count := 0
	for i := range typ.NumMethod() {
		method := typ.Method(i)
		if strings.HasPrefix(method.Name, "Get") &&
			method.Type.NumIn() == 1 &&
			method.Type.NumOut() == 1 {
			value.Method(i).Call(nil)
			count++
		}
	}
	if count == 0 {
		t.Fatalf("%T has no generated getters", msg)
	}
}

func callGeneratedNilGetters(t *testing.T) {
	t.Helper()
	nilMessages := []any{
		(*GetClassificationQualityRequest)(nil),
		(*GetClassificationQualitySamplesRequest)(nil),
		(*GetClassificationQualityResponse)(nil),
		(*GetClassificationQualitySamplesResponse)(nil),
		(*ClassificationQualitySummary)(nil),
		(*ClassificationQualityTimeBucket)(nil),
		(*ClassificationDimensionDrift)(nil),
		(*ClassificationValueDrift)(nil),
		(*ClassificationQualityWarning)(nil),
		(*ClassificationQualitySample)(nil),
	}
	for _, msg := range nilMessages {
		callGeneratedGetters(t, msg)
	}
}

func requireEqual[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}
