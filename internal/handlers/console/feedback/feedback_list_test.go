package feedback

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

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
}
