// ptrext:file-allow test fixtures build proto-request pointers
// SPDX-License-Identifier: Apache-2.0

package digestsubscription

import (
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	digestsubrepo "github.com/Phixsura/attune/internal/repo/digestsubscription"
)

func digestSubFixture() digestsubrepo.Subscription {
	now := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
	return digestsubrepo.Subscription{
		Enabled:        true,
		Frequency:      "weekly",
		SendHour:       8,
		Byweekday:      ptrext.Of(3),
		Timezone:       ptrext.Of("Asia/Shanghai"),
		LLMMinFeedback: 6,
		NextRunAt:      ptrext.Of(now),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

func TestNormalizeUpsert(t *testing.T) {
	t.Run("daily valid defaults llm_min", func(t *testing.T) {
		sub, err := normalizeUpsert(&attunev1.UpsertDigestSubscriptionRequest{
			Enabled: true, Frequency: "daily", SendHour: 9,
		})
		if err != nil {
			t.Fatal(err)
		}
		if sub.Frequency != "daily" || sub.SendHour != 9 || sub.LLMMinFeedback != 6 {
			t.Fatalf("sub = %#v", sub)
		}
		if sub.Byweekday != nil {
			t.Fatal("daily must not set byweekday")
		}
	})

	t.Run("weekly requires byweekday", func(t *testing.T) {
		if _, err := normalizeUpsert(&attunev1.UpsertDigestSubscriptionRequest{Frequency: "weekly", SendHour: 9}); err == nil {
			t.Fatal("expected error for weekly without byweekday")
		}
	})

	t.Run("weekly byweekday out of range", func(t *testing.T) {
		if _, err := normalizeUpsert(&attunev1.UpsertDigestSubscriptionRequest{
			Frequency: "weekly", SendHour: 9, Byweekday: ptrext.Of(int32(7)),
		}); err == nil {
			t.Fatal("expected error for byweekday=7")
		}
	})

	t.Run("weekly valid keeps explicit llm_min", func(t *testing.T) {
		sub, err := normalizeUpsert(&attunev1.UpsertDigestSubscriptionRequest{
			Frequency: "weekly", SendHour: 8, Byweekday: ptrext.Of(int32(1)), LlmMinFeedback: 10,
		})
		if err != nil {
			t.Fatal(err)
		}
		if sub.Byweekday == nil || ptrext.Indirect(sub.Byweekday) != 1 || sub.LLMMinFeedback != 10 {
			t.Fatalf("sub = %#v", sub)
		}
	})

	t.Run("rejects bad frequency", func(t *testing.T) {
		if _, err := normalizeUpsert(&attunev1.UpsertDigestSubscriptionRequest{Frequency: "hourly", SendHour: 9}); err == nil {
			t.Fatal("expected error for bad frequency")
		}
	})

	t.Run("rejects out-of-range send_hour", func(t *testing.T) {
		if _, err := normalizeUpsert(&attunev1.UpsertDigestSubscriptionRequest{Frequency: "daily", SendHour: 24}); err == nil {
			t.Fatal("expected error for send_hour=24")
		}
	})

	t.Run("rejects bad timezone", func(t *testing.T) {
		if _, err := normalizeUpsert(&attunev1.UpsertDigestSubscriptionRequest{
			Frequency: "daily", SendHour: 9, Timezone: ptrext.Of("Mars/Phobos"),
		}); err == nil {
			t.Fatal("expected error for invalid timezone")
		}
	})
}

func TestToDigestProto(t *testing.T) {
	sub := digestSubFixture()
	out := toDigestProto(sub)
	if out.GetFrequency() != "weekly" || out.GetSendHour() != 8 || !out.GetEnabled() {
		t.Fatalf("out = %#v", out)
	}
	if out.Byweekday == nil || out.GetByweekday() != 3 {
		t.Fatalf("byweekday = %v", out.Byweekday)
	}
	if out.Timezone == nil || out.GetTimezone() != "Asia/Shanghai" {
		t.Fatalf("timezone = %v", out.Timezone)
	}
	if out.NextRunAt == nil {
		t.Fatal("next_run_at should be set")
	}
}
