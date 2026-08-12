// SPDX-License-Identifier: Apache-2.0

package digest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/feedback"
)

var errAnomalyRead = errors.New("anomaly read down")

// fakeAnomalyReader cans the digest anomaly section source.
type fakeAnomalyReader struct {
	rows  []AnomalySummary
	err   error
	calls int
}

func (f *fakeAnomalyReader) OpenDigestAnomalies(context.Context, string, time.Time, time.Time) ([]AnomalySummary, error) {
	f.calls++
	return f.rows, f.err
}

func TestAggregateIncludesAnomalySection(t *testing.T) {
	ctx := context.Background()
	from, to := time.Now().Add(-24*time.Hour), time.Now()
	fb := fakeFeedback{
		stats: feedback.DigestWindowStats{Total: 4, Enriched: 4},
		rows:  []feedback.DigestFeedbackRow{{ID: 1, Title: "a"}},
	}
	agg := NewAggregator(fakeClusters{}, fb, fakeNamer{})
	reader := ptrext.Of(fakeAnomalyReader{rows: []AnomalySummary{{
		SliceDisplay: "severity=critical", Direction: "spike", Observed: 40, ExpectedMed: 12, EventID: "e1",
	}}})
	agg.SetAnomalyReader(reader)

	res, err := agg.Aggregate(ctx, AggInput{TenantID: "t", LLMMin: 6}, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Anomalies) != 1 || res.Anomalies[0].EventID != "e1" {
		t.Fatalf("anomaly section missing: %+v", res.Anomalies)
	}
	if reader.calls != 1 {
		t.Fatalf("reader must be consulted once, got %d", reader.calls)
	}
}

func TestAggregateAnomalyReadFailureIsBestEffort(t *testing.T) {
	ctx := context.Background()
	from, to := time.Now().Add(-24*time.Hour), time.Now()
	fb := fakeFeedback{
		stats: feedback.DigestWindowStats{Total: 4, Enriched: 4},
		rows:  []feedback.DigestFeedbackRow{{ID: 1, Title: "a"}},
	}
	agg := NewAggregator(fakeClusters{}, fb, fakeNamer{})
	agg.SetAnomalyReader(ptrext.Of(fakeAnomalyReader{err: errAnomalyRead}))

	res, err := agg.Aggregate(ctx, AggInput{TenantID: "t", LLMMin: 6}, from, to)
	if err != nil {
		t.Fatalf("anomaly read failure must never sink the digest: %v", err)
	}
	if res.Anomalies != nil {
		t.Fatalf("failed read must yield no section: %+v", res.Anomalies)
	}
}

func TestWindowAnomaliesNilReader(t *testing.T) {
	agg := NewAggregator(fakeClusters{}, fakeFeedback{}, fakeNamer{})
	if got := agg.windowAnomalies(context.Background(), "t", time.Now(), time.Now()); got != nil {
		t.Fatalf("nil reader must yield nil, got %+v", got)
	}
}
