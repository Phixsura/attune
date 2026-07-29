package enrich

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/webhooksub"
)

type fakeSubscriptionLister struct {
	byEvent map[string][]webhooksub.Subscription
	err     error
}

func (f *fakeSubscriptionLister) ListActiveByTenantEvent(_ context.Context, _, eventType string) ([]webhooksub.Subscription, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byEvent[eventType], nil
}

func subWithEvents(id string, events ...string) webhooksub.Subscription {
	return webhooksub.Subscription{
		ID:         uuid.MustParse(id),
		TenantID:   "t1",
		TargetURL:  "https://hooks.zapier.com/" + id[:8],
		Secret:     "0123456789abcdef",
		EventTypes: events,
		Status:     webhooksub.StatusActive,
	}
}

const (
	subA = "aaaaaaaa-0000-0000-0000-000000000001" // feedback.created only
	subB = "bbbbbbbb-0000-0000-0000-000000000002" // created + urgent
)

func subscriptionFixture() *fakeSubscriptionLister {
	a := subWithEvents(subA, domain.EventFeedbackCreated)
	b := subWithEvents(subB, domain.EventFeedbackCreated, domain.EventFeedbackUrgent)
	return ptrext.Of(fakeSubscriptionLister{byEvent: map[string][]webhooksub.Subscription{
		domain.EventFeedbackCreated: {a, b},
		domain.EventFeedbackUrgent:  {b},
	}})
}

func TestPlanSubscriptionRows_NonUrgent(t *testing.T) {
	e := NewEnricher(nil, nil, "")
	e.SetSubscriptions(subscriptionFixture())

	rows, err := e.planSubscriptionRows(context.Background(), sampleSnapshot(false), "trace-x")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("non-urgent rows: got %d want 2 (one per created-subscribed sub)", len(rows))
	}
	for _, r := range rows {
		if r.DestinationType != "subscription-webhook" {
			t.Errorf("destination_type: %q", r.DestinationType)
		}
		var env map[string]any
		if err := json.Unmarshal(r.Payload, &env); err != nil {
			t.Fatalf("payload: %v", err)
		}
		if env["event_type"] != domain.EventFeedbackCreated {
			t.Errorf("event_type: %v", env["event_type"])
		}
	}
	if rows[0].DestinationTarget != subA || rows[1].DestinationTarget != subB {
		t.Errorf("targets: %s, %s", rows[0].DestinationTarget, rows[1].DestinationTarget)
	}
}

func TestPlanSubscriptionRows_UrgentAddsUrgentEvent(t *testing.T) {
	e := NewEnricher(nil, nil, "")
	e.SetSubscriptions(subscriptionFixture())

	rows, err := e.planSubscriptionRows(context.Background(), sampleSnapshot(true), "trace-x")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	// subA: created; subB: created + urgent → 3 rows
	if len(rows) != 3 {
		t.Fatalf("urgent rows: got %d want 3", len(rows))
	}
	byEvent := map[string][]string{}
	for _, r := range rows {
		var env map[string]any
		if err := json.Unmarshal(r.Payload, &env); err != nil {
			t.Fatalf("payload: %v", err)
		}
		et := env["event_type"].(string)
		byEvent[et] = append(byEvent[et], r.DestinationTarget)
	}
	if len(byEvent[domain.EventFeedbackCreated]) != 2 {
		t.Errorf("created rows: %v", byEvent[domain.EventFeedbackCreated])
	}
	if len(byEvent[domain.EventFeedbackUrgent]) != 1 || byEvent[domain.EventFeedbackUrgent][0] != subB {
		t.Errorf("urgent rows: %v", byEvent[domain.EventFeedbackUrgent])
	}
}

func TestPlanSubscriptionRows_NilListerNoRows(t *testing.T) {
	e := NewEnricher(nil, nil, "")
	rows, err := e.planSubscriptionRows(context.Background(), sampleSnapshot(true), "trace-x")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("nil lister must add no rows, got %d", len(rows))
	}
}
