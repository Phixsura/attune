// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	anomalyrepo "github.com/Phixsura/attune/internal/repo/anomaly"
	digestsubrepo "github.com/Phixsura/attune/internal/repo/digestsubscription"
	"github.com/Phixsura/attune/internal/repo/tenant"
)

var errAnomalyWiring = errors.New("wiring boom")

// fakeDigestAnomalyRepo cans OpenDigestAnomaliesInWindow.
type fakeDigestAnomalyRepo struct {
	rows []anomalyrepo.DigestAnomaly
	err  error
}

func (f fakeDigestAnomalyRepo) OpenDigestAnomaliesInWindow(context.Context, string, time.Time, time.Time) ([]anomalyrepo.DigestAnomaly, error) {
	return f.rows, f.err
}

func TestDigestAnomalyReaderMapsRows(t *testing.T) {
	r := digestAnomalyReader{repo: fakeDigestAnomalyRepo{rows: []anomalyrepo.DigestAnomaly{{
		SliceDisplay: "severity=critical", Direction: "spike",
		Observed: 40, ExpectedMed: 12, EventID: "ev-1",
	}}}}
	out, err := r.OpenDigestAnomalies(context.Background(), "t1", time.Now(), time.Now())
	if err != nil || len(out) != 1 {
		t.Fatalf("OpenDigestAnomalies: %v %v", err, out)
	}
	got := out[0]
	if got.SliceDisplay != "severity=critical" || got.Direction != "spike" ||
		got.Observed != 40 || got.ExpectedMed != 12 || got.EventID != "ev-1" {
		t.Fatalf("field mapping wrong: %+v", got)
	}
}

func TestDigestAnomalyReaderPropagatesError(t *testing.T) {
	r := digestAnomalyReader{repo: fakeDigestAnomalyRepo{err: errAnomalyWiring}}
	if _, err := r.OpenDigestAnomalies(context.Background(), "t1", time.Now(), time.Now()); err == nil {
		t.Fatal("repo error must propagate")
	}
}

// fakeEnrichRepo cans GetEnrichConfig.
type fakeEnrichRepo struct {
	cfg tenant.EnrichConfig
	err error
}

func (f fakeEnrichRepo) GetEnrichConfig(context.Context, string) (tenant.EnrichConfig, error) {
	return f.cfg, f.err
}

func TestAnomalyEnrichReaderMapsDimensions(t *testing.T) {
	r := anomalyEnrichReader{repo: fakeEnrichRepo{cfg: tenant.EnrichConfig{}}}
	view, err := r.GetEnrichConfig(context.Background(), "t1")
	if err != nil || view.Dimensions != nil {
		t.Fatalf("GetEnrichConfig: %v %+v", err, view)
	}
	r2 := anomalyEnrichReader{repo: fakeEnrichRepo{err: errAnomalyWiring}}
	if _, err := r2.GetEnrichConfig(context.Background(), "t1"); err == nil {
		t.Fatal("repo error must propagate")
	}
}

// fakeDigestSubReader cans GetByTenant.
type fakeDigestSubReader struct {
	sub *digestsubrepo.Subscription
	err error
}

func (f fakeDigestSubReader) GetByTenant(context.Context, string) (*digestsubrepo.Subscription, error) {
	return f.sub, f.err
}

func TestAnomalyDigestCheckerBranches(t *testing.T) {
	// Enabled subscription → true.
	c := anomalyDigestChecker{repo: fakeDigestSubReader{sub: ptrext.Of(digestsubrepo.Subscription{Enabled: true})}}
	has, err := c.GetByTenant(context.Background(), "t1")
	if err != nil || !has {
		t.Fatalf("enabled sub must report true: %v %v", has, err)
	}
	// Disabled subscription → false.
	c = anomalyDigestChecker{repo: fakeDigestSubReader{sub: ptrext.Of(digestsubrepo.Subscription{})}}
	has, err = c.GetByTenant(context.Background(), "t1")
	if err != nil || has {
		t.Fatalf("disabled sub must report false: %v %v", has, err)
	}
	// Not found → false without error.
	c = anomalyDigestChecker{repo: fakeDigestSubReader{err: digestsubrepo.ErrNotFound}}
	has, err = c.GetByTenant(context.Background(), "t1")
	if err != nil || has {
		t.Fatalf("no sub must report false, nil: %v %v", has, err)
	}
	// Other errors propagate.
	c = anomalyDigestChecker{repo: fakeDigestSubReader{err: errAnomalyWiring}}
	if _, err := c.GetByTenant(context.Background(), "t1"); err == nil {
		t.Fatal("repo error must propagate")
	}
}

func TestStartAnomalyWorkerSpawnsAndStops(t *testing.T) {
	pool := newUnreachableServerPool(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // worker loop must exit immediately on a dead context
	startAnomalyWorker(ctx, pool, "https://console.test", time.Hour, 3)
	// Give the supervised goroutine a beat to observe cancellation.
	time.Sleep(50 * time.Millisecond)
}

func TestStartDigestWorkerSpawnsAndStops(t *testing.T) {
	pool := newUnreachableServerPool(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	startDigestWorker(ctx, pool, nil, "https://console.test")
	time.Sleep(50 * time.Millisecond)
}
