// SPDX-License-Identifier: Apache-2.0

package searchquality

import (
	"errors"
	"fmt"
)

// ValidateFixtures verifies that golden queries, expectations, and result sets
// reference a coherent synthetic feedback corpus.
func ValidateFixtures(
	feedbackRows []FeedbackFixture,
	queryRows []QueryFixture,
	expectedRows []ExpectedQuery,
	resultsByQuery map[string][]RankedResult,
) error {
	feedbackByID, err := feedbackByID(feedbackRows)
	if err != nil {
		return err
	}
	queryByID, err := queryByID(queryRows)
	if err != nil {
		return err
	}

	var errs []error
	expectedSeen := make(map[string]struct{}, len(expectedRows))
	for _, expected := range expectedRows {
		errs = appendExpectedFixtureErrors(errs, expected, expectedSeen, queryByID, feedbackByID)
	}
	for queryID := range queryByID {
		if _, ok := expectedSeen[queryID]; !ok {
			errs = append(errs, fmt.Errorf("query %q has no relevance expectations", queryID))
		}
		if _, ok := resultsByQuery[queryID]; !ok {
			errs = append(errs, fmt.Errorf("query %q has no result set", queryID))
		}
	}
	for queryID, results := range resultsByQuery {
		query, ok := queryByID[queryID]
		if !ok {
			errs = append(errs, fmt.Errorf("result set references unknown query %q", queryID))
			continue
		}
		errs = appendResultFixtureErrors(errs, query, results, feedbackByID)
	}
	return errors.Join(errs...)
}

func feedbackByID(rows []FeedbackFixture) (map[int64]FeedbackFixture, error) {
	out := make(map[int64]FeedbackFixture, len(rows))
	var errs []error
	for _, row := range rows {
		if row.ID == 0 {
			errs = append(errs, errors.New("feedback fixture has empty id"))
			continue
		}
		if row.TenantID == "" {
			errs = append(errs, fmt.Errorf("feedback %d has empty tenant_id", row.ID))
		}
		if row.Content == "" && row.EnrichedTitle == "" {
			errs = append(errs, fmt.Errorf("feedback %d has neither content nor enriched_title", row.ID))
		}
		if _, ok := out[row.ID]; ok {
			errs = append(errs, fmt.Errorf("duplicate feedback id %d", row.ID))
			continue
		}
		out[row.ID] = row
	}
	return out, errors.Join(errs...)
}

func queryByID(rows []QueryFixture) (map[string]QueryFixture, error) {
	out := make(map[string]QueryFixture, len(rows))
	var errs []error
	for _, row := range rows {
		if row.ID == "" {
			errs = append(errs, errors.New("query fixture has empty id"))
			continue
		}
		if row.TenantID == "" {
			errs = append(errs, fmt.Errorf("query %q has empty tenant_id", row.ID))
		}
		if row.Query == "" {
			errs = append(errs, fmt.Errorf("query %q has empty query text", row.ID))
		}
		if _, ok := out[row.ID]; ok {
			errs = append(errs, fmt.Errorf("duplicate query id %q", row.ID))
			continue
		}
		out[row.ID] = row
	}
	return out, errors.Join(errs...)
}

func appendExpectedFixtureErrors(
	errs []error,
	expected ExpectedQuery,
	seen map[string]struct{},
	queryByID map[string]QueryFixture,
	feedbackByID map[int64]FeedbackFixture,
) []error {
	if expected.ID == "" {
		return append(errs, errors.New("expected fixture has empty id"))
	}
	if _, ok := seen[expected.ID]; ok {
		errs = append(errs, fmt.Errorf("duplicate expected query id %q", expected.ID))
	} else {
		seen[expected.ID] = struct{}{}
	}
	query, ok := queryByID[expected.ID]
	if !ok {
		errs = append(errs, fmt.Errorf("expected fixture references unknown query %q", expected.ID))
	} else if expected.TenantID != "" && expected.TenantID != query.TenantID {
		errs = append(errs, fmt.Errorf("expected %q tenant_id %q does not match query tenant_id %q", expected.ID, expected.TenantID, query.TenantID))
	}
	if len(expected.RelevantFeedbackIDs) == 0 {
		errs = append(errs, fmt.Errorf("expected %q has no relevant feedback ids", expected.ID))
	}
	errs = appendFeedbackRefErrors(errs, expected.ID, expected.TenantID, "relevant_feedback_ids", expected.RelevantFeedbackIDs, feedbackByID, true)
	errs = appendFeedbackRefErrors(errs, expected.ID, expected.TenantID, "allowed_feedback_ids", expected.AllowedFeedbackIDs, feedbackByID, true)
	errs = appendFeedbackRefErrors(errs, expected.ID, "", "must_not_match_feedback_ids", expected.MustNotMatchIDs, feedbackByID, false)
	return appendRelevantAllowedErrors(errs, expected)
}

func appendFeedbackRefErrors(
	errs []error,
	queryID string,
	tenantID string,
	field string,
	ids []int64,
	feedbackByID map[int64]FeedbackFixture,
	requireTenant bool,
) []error {
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			errs = append(errs, fmt.Errorf("expected %q has duplicate %s id %d", queryID, field, id))
			continue
		}
		seen[id] = struct{}{}
		feedbackRow, ok := feedbackByID[id]
		if !ok {
			errs = append(errs, fmt.Errorf("expected %q %s references unknown feedback id %d", queryID, field, id))
			continue
		}
		if requireTenant && tenantID != "" && feedbackRow.TenantID != tenantID {
			errs = append(errs, fmt.Errorf("expected %q %s id %d belongs to tenant %q", queryID, field, id, feedbackRow.TenantID))
		}
	}
	return errs
}

func appendRelevantAllowedErrors(errs []error, expected ExpectedQuery) []error {
	if len(expected.AllowedFeedbackIDs) == 0 {
		return errs
	}
	allowed := idSet(expected.AllowedFeedbackIDs)
	for _, id := range expected.RelevantFeedbackIDs {
		if _, ok := allowed[id]; !ok {
			errs = append(errs, fmt.Errorf("expected %q relevant id %d is missing from allowed_feedback_ids", expected.ID, id))
		}
	}
	return errs
}

func appendResultFixtureErrors(
	errs []error,
	query QueryFixture,
	results []RankedResult,
	feedbackByID map[int64]FeedbackFixture,
) []error {
	seen := make(map[int64]struct{}, len(results))
	for _, result := range results {
		if _, ok := seen[result.FeedbackID]; ok {
			errs = append(errs, fmt.Errorf("result set %q has duplicate feedback id %d", query.ID, result.FeedbackID))
			continue
		}
		seen[result.FeedbackID] = struct{}{}
		feedbackRow, ok := feedbackByID[result.FeedbackID]
		if !ok {
			errs = append(errs, fmt.Errorf("result set %q references unknown feedback id %d", query.ID, result.FeedbackID))
			continue
		}
		if result.TenantID != "" && result.TenantID != feedbackRow.TenantID {
			errs = append(errs, fmt.Errorf("result set %q feedback id %d reports tenant %q but fixture tenant is %q", query.ID, result.FeedbackID, result.TenantID, feedbackRow.TenantID))
		}
		if feedbackRow.TenantID != query.TenantID {
			errs = append(errs, fmt.Errorf("result set %q leaks feedback id %d from tenant %q", query.ID, result.FeedbackID, feedbackRow.TenantID))
		}
	}
	return errs
}
