// ptrext:file-allow test assertions validating pointer values
package feedback

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

func TestQueryStr(t *testing.T) {
	t.Parallel()

	t.Run("returns value when present", func(t *testing.T) {
		t.Parallel()
		q := url.Values{"key": []string{"value"}}
		result := queryStr(q, "key")
		require.NotNil(t, result)
		require.Equal(t, "value", *result)
	})

	t.Run("returns nil when key missing", func(t *testing.T) {
		t.Parallel()
		q := url.Values{}
		result := queryStr(q, "key")
		require.Nil(t, result)
	})

	t.Run("returns nil when value empty", func(t *testing.T) {
		t.Parallel()
		q := url.Values{"key": []string{""}}
		result := queryStr(q, "key")
		require.Nil(t, result)
	})
}

func TestQueryInt32(t *testing.T) {
	t.Parallel()

	t.Run("returns value when valid int", func(t *testing.T) {
		t.Parallel()
		q := url.Values{"key": []string{"42"}}
		result := queryInt32(q, "key")
		require.NotNil(t, result)
		require.Equal(t, int32(42), *result)
	})

	t.Run("returns nil when invalid int", func(t *testing.T) {
		t.Parallel()
		q := url.Values{"key": []string{"not-a-number"}}
		result := queryInt32(q, "key")
		require.Nil(t, result)
	})

	t.Run("returns nil when key missing", func(t *testing.T) {
		t.Parallel()
		q := url.Values{}
		result := queryInt32(q, "key")
		require.Nil(t, result)
	})
}

func TestQueryBool(t *testing.T) {
	t.Parallel()

	t.Run("returns true for 'true'", func(t *testing.T) {
		t.Parallel()
		q := url.Values{"key": []string{"true"}}
		result := queryBool(q, "key")
		require.NotNil(t, result)
		require.True(t, *result)
	})

	t.Run("returns true for '1'", func(t *testing.T) {
		t.Parallel()
		q := url.Values{"key": []string{"1"}}
		result := queryBool(q, "key")
		require.NotNil(t, result)
		require.True(t, *result)
	})

	t.Run("returns false for 'false'", func(t *testing.T) {
		t.Parallel()
		q := url.Values{"key": []string{"false"}}
		result := queryBool(q, "key")
		require.NotNil(t, result)
		require.False(t, *result)
	})

	t.Run("returns nil when key missing", func(t *testing.T) {
		t.Parallel()
		q := url.Values{}
		result := queryBool(q, "key")
		require.Nil(t, result)
	})

	t.Run("returns nil when value empty", func(t *testing.T) {
		t.Parallel()
		q := url.Values{"key": []string{""}}
		result := queryBool(q, "key")
		require.Nil(t, result)
	})
}

func TestQueryIDs(t *testing.T) {
	t.Parallel()

	t.Run("parses comma separated repeated values and skips invalid", func(t *testing.T) {
		t.Parallel()
		q := url.Values{"ids": []string{"1, nope, 2", "0,-1,3"}}

		require.Equal(t, []int64{1, 2, 3}, queryIDs(q, "ids"))
	})

	t.Run("caps returned ids at fifty", func(t *testing.T) {
		t.Parallel()
		values := make([]string, 0, 55)
		for i := 1; i <= 55; i++ {
			values = append(values, strconv.Itoa(i))
		}
		q := url.Values{"ids": []string{strings.Join(values, ",")}}

		got := queryIDs(q, "ids")

		require.Len(t, got, 50)
		require.Equal(t, int64(1), got[0])
		require.Equal(t, int64(50), got[49])
	})

	t.Run("returns nil when missing", func(t *testing.T) {
		t.Parallel()
		require.Nil(t, queryIDs(url.Values{}, "ids"))
	})
}

func TestBindListRequestSkipsEmptyDynamicAttrs(t *testing.T) {
	t.Parallel()

	req := ptrext.Of(attunev1.ListFeedbackRequest{})
	httpReq := httptest.NewRequest(http.MethodGet, "/feedback?severity=&severity=critical&limit=20", nil)

	require.NoError(t, BindListRequest(httpReq, req))
	require.Len(t, req.GetAttrs(), 1)
	require.Equal(t, "severity", req.GetAttrs()[0].GetDim())
	require.Equal(t, "critical", req.GetAttrs()[0].GetValue())
	require.Equal(t, int32(20), req.GetLimit())
}

func TestQueryFloat64(t *testing.T) {
	t.Parallel()

	t.Run("returns value when valid", func(t *testing.T) {
		t.Parallel()
		q := url.Values{"confidence_lte": []string{"0.42"}}

		got := queryFloat64(q, "confidence_lte")

		require.NotNil(t, got)
		require.InDelta(t, 0.42, *got, 1e-9)
	})

	t.Run("returns nil when invalid or missing", func(t *testing.T) {
		t.Parallel()
		require.Nil(t, queryFloat64(url.Values{"confidence_lte": []string{"bad"}}, "confidence_lte"))
		require.Nil(t, queryFloat64(url.Values{}, "confidence_lte"))
	})
}

func TestBindListRequestLimitBounds(t *testing.T) {
	t.Parallel()

	t.Run("sets int32 limit", func(t *testing.T) {
		t.Parallel()

		req := ptrext.Of(attunev1.ListFeedbackRequest{})
		r := httptest.NewRequest(http.MethodGet, "/?limit=2147483647", nil)

		require.NoError(t, BindListRequest(r, req))
		require.Equal(t, int32(2147483647), req.GetLimit())
	})

	t.Run("ignores int32 overflow", func(t *testing.T) {
		t.Parallel()

		req := ptrext.Of(attunev1.ListFeedbackRequest{})
		r := httptest.NewRequest(http.MethodGet, "/?limit=2147483648", nil)

		require.NoError(t, BindListRequest(r, req))
		require.Nil(t, req.Limit)
	})

	t.Run("workflow_state filter", func(t *testing.T) {
		t.Parallel()
		req := ptrext.Of(attunev1.ListFeedbackRequest{})
		r := httptest.NewRequest(http.MethodGet, "/?workflow_state=s-1", nil)

		require.NoError(t, BindListRequest(r, req))
		require.Equal(t, "s-1", req.GetWorkflowStateId())
	})

	t.Run("workflow_category filter", func(t *testing.T) {
		t.Parallel()
		req := ptrext.Of(attunev1.ListFeedbackRequest{})
		r := httptest.NewRequest(http.MethodGet, "/?workflow_category=open", nil)

		require.NoError(t, BindListRequest(r, req))
		require.Equal(t, "open", req.GetWorkflowCategory())
	})

	t.Run("enrichment_status filter", func(t *testing.T) {
		t.Parallel()
		req := ptrext.Of(attunev1.ListFeedbackRequest{})
		r := httptest.NewRequest(http.MethodGet, "/?enrichment_status=failed", nil)

		require.NoError(t, BindListRequest(r, req))
		require.Equal(t, "failed", req.GetEnrichmentStatus())
	})

	t.Run("terminal_failed_only filter true", func(t *testing.T) {
		t.Parallel()
		req := ptrext.Of(attunev1.ListFeedbackRequest{})
		r := httptest.NewRequest(http.MethodGet, "/?terminal_failed_only=true", nil)

		require.NoError(t, BindListRequest(r, req))
		require.True(t, req.GetTerminalFailedOnly())
	})

	t.Run("terminal_failed_only filter 1", func(t *testing.T) {
		t.Parallel()
		req := ptrext.Of(attunev1.ListFeedbackRequest{})
		r := httptest.NewRequest(http.MethodGet, "/?terminal_failed_only=1", nil)

		require.NoError(t, BindListRequest(r, req))
		require.True(t, req.GetTerminalFailedOnly())
	})

	t.Run("terminal_failed_only filter false", func(t *testing.T) {
		t.Parallel()
		req := ptrext.Of(attunev1.ListFeedbackRequest{})
		r := httptest.NewRequest(http.MethodGet, "/?terminal_failed_only=false", nil)

		require.NoError(t, BindListRequest(r, req))
		require.False(t, req.GetTerminalFailedOnly())
	})

	t.Run("combined filters", func(t *testing.T) {
		t.Parallel()
		req := ptrext.Of(attunev1.ListFeedbackRequest{})
		r := httptest.NewRequest(http.MethodGet, "/?enrichment_status=failed&terminal_failed_only=true&limit=50", nil)

		require.NoError(t, BindListRequest(r, req))
		require.Equal(t, "failed", req.GetEnrichmentStatus())
		require.True(t, req.GetTerminalFailedOnly())
		require.Equal(t, int32(50), req.GetLimit())
	})
}
