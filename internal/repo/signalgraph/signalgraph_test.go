package signalgraph

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSubjectEventEvidenceIDsAreBoundedAndDeduped(t *testing.T) {
	t.Parallel()

	events := []SubjectEvent{
		{FeedbackIDs: []int64{0, 101, 102, 103, 104, 105, 106}},
		{FeedbackIDs: []int64{102, 201, -1, 202}},
	}

	got := subjectEventEvidenceIDs(events)

	require.Equal(t, []int64{101, 102, 103, 104, 105, 201, 202}, got)
}

func TestLimitedPositiveIDsHonorsLimit(t *testing.T) {
	t.Parallel()

	require.Equal(t, []int64{7, 8}, limitedPositiveIDs([]int64{-1, 0, 7, 8, 9}, 2))
	require.Nil(t, limitedPositiveIDs([]int64{1, 2}, 0))
}
