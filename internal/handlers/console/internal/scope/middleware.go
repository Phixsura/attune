// Package scope provides middleware for API key scope enforcement.
package scope

import (
	"fmt"
	"net/http"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/apikey"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/pkg/logext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// RequireScope returns middleware that checks API key scopes.
// Session requests pass through (RBAC handles them).
func RequireScope(required domain.Scope) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			auth, ok := apikey.FromContextSafe(ctx)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			if !domain.HasScope(auth.Scopes, required) {
				metrics.APIKeyScopeDeniedTotal.WithLabelValues(string(required)).Inc()
				logext.Warnf(ctx, "[scope] deny,key_id:%s,required:%s", auth.KeyID, required)
				dispatcher.Reject(ctx, w, http.StatusForbidden,
					attunev1.ErrorCode_FORBIDDEN,
					fmt.Sprintf("missing scope: %s", required))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
