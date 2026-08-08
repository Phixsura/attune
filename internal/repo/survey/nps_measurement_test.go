// SPDX-License-Identifier: Apache-2.0

package survey

import "testing"

func TestNPSRunMeasurementKeyTracksComparableDefinition(t *testing.T) {
	t.Parallel()

	baseline := npsMeasurementFixture(
		"How likely are you to recommend us to a colleague?",
		"11111111-1111-1111-1111-111111111111",
		"500",
		"sample-seed-123456",
		"14",
		"90",
	)
	key := npsRunMeasurementKey(baseline)
	if key == "" || key[:7] != "nps:v4:" {
		t.Fatalf("npsRunMeasurementKey() = %q, want versioned key", key)
	}

	sameDefinition := npsMeasurementFixture(
		"How likely are you to recommend us to a colleague?",
		"11111111-1111-1111-1111-111111111111",
		"500",
		"sample-seed-123456",
		"14",
		"90",
	)
	sameDefinition["detractor_owner_member_id"] = "22222222-2222-2222-2222-222222222222"
	if got := npsRunMeasurementKey(sameDefinition); got != key {
		t.Fatalf("recovery-owner-only update key = %q, want %q", got, key)
	}
	qualificationPolicyOnly := npsMeasurementFixture(
		"How likely are you to recommend us to a colleague?",
		"11111111-1111-1111-1111-111111111111",
		"500",
		"sample-seed-123456",
		"14",
		"90",
	)
	qualificationPolicyOnly["minimum_completed_responses"] = "100"
	qualificationPolicyOnly["minimum_response_rate_percent"] = "25"
	if got := npsRunMeasurementKey(qualificationPolicyOnly); got != key {
		t.Fatalf("qualification-policy-only update key = %q, want %q", got, key)
	}

	for name, definition := range map[string]map[string]any{
		"question": npsMeasurementFixture(
			"How likely are you to recommend a teammate?",
			"11111111-1111-1111-1111-111111111111",
			"500",
			"sample-seed-123456",
			"14",
			"90",
		),
		"cohort": npsMeasurementFixture(
			"How likely are you to recommend us to a colleague?",
			"33333333-3333-3333-3333-333333333333",
			"500",
			"sample-seed-123456",
			"14",
			"90",
		),
		"sample": npsMeasurementFixture(
			"How likely are you to recommend us to a colleague?",
			"11111111-1111-1111-1111-111111111111",
			"250",
			"rotated-sample-seed",
			"14",
			"90",
		),
		"collection window": npsMeasurementFixture(
			"How likely are you to recommend us to a colleague?",
			"11111111-1111-1111-1111-111111111111",
			"500",
			"sample-seed-123456",
			"21",
			"90",
		),
		"contact cooldown": npsMeasurementFixture(
			"How likely are you to recommend us to a colleague?",
			"11111111-1111-1111-1111-111111111111",
			"500",
			"sample-seed-123456",
			"14",
			"30",
		),
		"recurring allocation": func() map[string]any {
			definition := npsMeasurementFixture(
				"How likely are you to recommend us to a colleague?",
				"11111111-1111-1111-1111-111111111111",
				"500",
				"sample-seed-123456",
				"14",
				"90",
			)
			definition["recurrence_interval_days"] = "90"
			definition["recurrence_sampling_percent"] = "50"
			return definition
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			if got := npsRunMeasurementKey(definition); got == key {
				t.Fatalf("changed %s did not change measurement key", name)
			}
		})
	}

	if got := npsRunMeasurementKey(map[string]any{}); got != "" {
		t.Fatalf("incomplete definition key = %q, want empty", got)
	}
}

func TestNPSRunMeasurementKeyTracksContentRevision(t *testing.T) {
	t.Parallel()

	legacy := npsMeasurementFixture(
		"How likely are you to recommend us to a colleague?",
		"11111111-1111-1111-1111-111111111111",
		"500",
		"sample-seed-123456",
		"14",
		"90",
	)
	delete(legacy["campaign"].(map[string]any), "nps_content_revision")
	if got := npsRunMeasurementKey(legacy); got != "" {
		t.Fatalf("legacy definition without content revision key = %q, want empty", got)
	}

	current := npsMeasurementFixture(
		"How likely are you to recommend us to a colleague?",
		"11111111-1111-1111-1111-111111111111",
		"500",
		"sample-seed-123456",
		"14",
		"90",
	)
	currentKey := npsRunMeasurementKey(current)
	current["campaign"].(map[string]any)["nps_content_revision"] = "nps-v2"
	if got := npsRunMeasurementKey(current); got == currentKey {
		t.Fatalf("content-revision-only update key = %q, want a new baseline", got)
	}
}

func TestNPSRunMeasurementReadinessUsesFrozenOperatingEvidence(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		run  NPSCampaignRun
		want string
	}{
		"scheduled": {
			run:  NPSCampaignRun{Status: NPSRunScheduled},
			want: NPSMeasurementPending,
		},
		"collecting remains preliminary": {
			run: NPSCampaignRun{
				Status: NPSRunCollecting, InvitationCount: 100, MinimumCompletedResponses: 30, MinimumResponseRatePercent: 10,
			},
			want: NPSMeasurementPreliminary,
		},
		"closed below completed threshold is directional": {
			run: NPSCampaignRun{
				Status: NPSRunClosed, InvitationCount: 100, MinimumCompletedResponses: 30, MinimumResponseRatePercent: 10,
			},
			want: NPSMeasurementDirectional,
		},
		"closed below response-rate threshold is directional": {
			run: NPSCampaignRun{
				Status: NPSRunClosed, InvitationCount: 100, MinimumCompletedResponses: 30, MinimumResponseRatePercent: 40,
			},
			want: NPSMeasurementDirectional,
		},
		"closed with both frozen thresholds is qualified": {
			run: NPSCampaignRun{
				Status: NPSRunClosed, InvitationCount: 100, MinimumCompletedResponses: 30, MinimumResponseRatePercent: 10,
			},
			want: NPSMeasurementQualified,
		},
		"redaction takes precedence over qualification": {
			run: NPSCampaignRun{
				Status: NPSRunClosed, InvitationCount: 100, RedactedResponseCount: 1, MinimumCompletedResponses: 30, MinimumResponseRatePercent: 10,
			},
			want: NPSMeasurementRedacted,
		},
		"invalid frozen definition is unavailable": {
			run:  NPSCampaignRun{Status: NPSRunClosed, InvitationCount: 100},
			want: NPSMeasurementUnavailable,
		},
		"failed is unavailable": {
			run:  NPSCampaignRun{Status: NPSRunFailed},
			want: NPSMeasurementUnavailable,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			completed := 30
			if name == "closed below completed threshold is directional" {
				completed = 29
			}
			if name == "closed below response-rate threshold is directional" {
				completed = 30
			}
			got := applyNPSCampaignRunMetrics(test.run, npsCampaignRunMetrics{StartedCount: 40, CompletedCount: completed})
			if got.MeasurementReadiness != test.want {
				t.Fatalf("MeasurementReadiness = %q, want %q", got.MeasurementReadiness, test.want)
			}
			if got.InvitationCount == 100 && got.CompletedResponseRate != float64(completed)/100 {
				t.Fatalf("CompletedResponseRate = %v, want %v", got.CompletedResponseRate, float64(completed)/100)
			}
			if got.InvitationCount == 100 && got.HostedVisitRate != 0.4 {
				t.Fatalf("HostedVisitRate = %v, want 0.4", got.HostedVisitRate)
			}
			if got.ResponseRate != got.HostedVisitRate {
				t.Fatalf("ResponseRate = %v, want compatibility alias %v", got.ResponseRate, got.HostedVisitRate)
			}
		})
	}
}

func TestApplyNPSCampaignRunMetricsMarksNPSAvailabilityFromCurrentBuckets(t *testing.T) {
	t.Parallel()

	withoutResponses := applyNPSCampaignRunMetrics(NPSCampaignRun{}, npsCampaignRunMetrics{})
	if withoutResponses.NPSAvailable || withoutResponses.NPS != 0 {
		t.Fatalf("empty NPS metrics = available %v, score %v; want unavailable/zero", withoutResponses.NPSAvailable, withoutResponses.NPS)
	}

	withResponses := applyNPSCampaignRunMetrics(NPSCampaignRun{}, npsCampaignRunMetrics{
		DetractorCount: 1,
		PassiveCount:   2,
		PromoterCount:  7,
	})
	if !withResponses.NPSAvailable || withResponses.NPS != 60 {
		t.Fatalf("current NPS metrics = available %v, score %v; want available/60", withResponses.NPSAvailable, withResponses.NPS)
	}
}

func TestNPSRunRecipientLimitDistributesRecurringPulses(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		maximumRecipients int
		availableContacts int
		eligibleContacts  int
		samplingPercent   int
		want              int
	}{
		"quarter of available contacts":              {500, 100, 100, 25, 25},
		"cooldown does not shrink the planned share": {500, 100, 75, 25, 25},
		"cap remains a blast radius guardrail":       {10, 100, 100, 25, 10},
		"small audiences still receive one":          {500, 3, 3, 10, 1},
		"no eligible contacts":                       {500, 100, 0, 25, 0},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := npsRunRecipientLimit(tc.maximumRecipients, tc.availableContacts, tc.eligibleContacts, tc.samplingPercent); got != tc.want {
				t.Fatalf("npsRunRecipientLimit() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestParseNPSIntRejectsValuesOutsideProtoInt32Range(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"2147483648", "-2147483649"} {
		if _, err := parseNPSInt(raw); err == nil {
			t.Fatalf("parseNPSInt(%q) returned nil error for an out-of-range value", raw)
		}
	}
	for _, raw := range []string{"2147483647", "-2147483648"} {
		if _, err := parseNPSInt(raw); err != nil {
			t.Fatalf("parseNPSInt(%q) returned error: %v", raw, err)
		}
	}
}

func npsMeasurementFixture(question, cohortID, maximumRecipients, sampleSeed, collectionDays, contactCooldownDays string) map[string]any {
	return map[string]any{
		"campaign": map[string]any{
			"survey_type":              TypeNPS,
			"nps_content_revision":     "nps-v1",
			"min_days_between_contact": contactCooldownDays,
			"content": map[string]any{
				"question": question,
			},
		},
		"cohort_id":                   cohortID,
		"maximum_run_recipients":      maximumRecipients,
		"sample_seed":                 sampleSeed,
		"collection_days":             collectionDays,
		"recurrence_interval_days":    "0",
		"recurrence_sampling_percent": "0",
	}
}
