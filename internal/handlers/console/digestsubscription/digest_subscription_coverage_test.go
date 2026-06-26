// ptrext:file-allow test-fixtures
// SPDX-License-Identifier: Apache-2.0

package digestsubscription

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	digestsubrepo "github.com/Phixsura/attune/internal/repo/digestsubscription"
	"github.com/Phixsura/attune/internal/repo/tenant"
)

// ---------------------------------------------------------------------------
// Configurable fakes for error-path testing. The audit_test.go fakes only
// support success paths, so we need variants that can return injected errors.
// ---------------------------------------------------------------------------

type configurableSubRepo struct {
	getSub    *digestsubrepo.Subscription
	getErr    error
	upsertSub *digestsubrepo.Subscription
	upsertErr error
	deleteErr error
}

func (f *configurableSubRepo) GetByTenant(context.Context, string) (*digestsubrepo.Subscription, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.getSub == nil {
		return nil, digestsubrepo.ErrNotFound
	}
	return f.getSub, nil
}

func (f *configurableSubRepo) Upsert(_ context.Context, s digestsubrepo.Subscription) (*digestsubrepo.Subscription, error) {
	if f.upsertErr != nil {
		return nil, f.upsertErr
	}
	if f.upsertSub != nil {
		return f.upsertSub, nil
	}
	out := s
	out.CreatedAt = time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
	out.UpdatedAt = out.CreatedAt
	return ptrext.Of(out), nil
}

func (f *configurableSubRepo) DeleteByTenant(context.Context, string) error {
	return f.deleteErr
}

type configurableTenantReader struct {
	t             *tenant.Tenant
	getErr        error
	clustering    bool
	clusterErr    error
	setClusterErr error
}

func (f *configurableTenantReader) GetByID(context.Context, string) (*tenant.Tenant, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.t, nil
}

func (f *configurableTenantReader) GetClusteringEnabled(context.Context, string) (bool, error) {
	return f.clustering, f.clusterErr
}

func (f *configurableTenantReader) SetClusteringEnabled(context.Context, string, bool) error {
	return f.setClusterErr
}

func coverageCtx() *dispatcher.RequestContext[*session.AuthCtx] {
	return ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}),
	})
}

// ---------------------------------------------------------------------------
// normalizeMin
// ---------------------------------------------------------------------------

func TestNormalizeMin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   int
		want int
	}{
		{"zero returns default", 0, defaultLLMMinFeedback},
		{"negative returns default", -1, defaultLLMMinFeedback},
		{"positive 1 kept", 1, 1},
		{"positive 10 kept", 10, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, normalizeMin(tt.in))
		})
	}
}

// ---------------------------------------------------------------------------
// weekdayField (direct unit tests)
// ---------------------------------------------------------------------------

func TestWeekdayField(t *testing.T) {
	t.Parallel()

	t.Run("non-weekly returns nil", func(t *testing.T) {
		t.Parallel()
		got, err := weekdayField("daily", ptrext.Of(attunev1.UpsertDigestSubscriptionRequest{}))
		require.NoError(t, err)
		require.Nil(t, got)
	})

	t.Run("weekly nil byweekday errors", func(t *testing.T) {
		t.Parallel()
		_, err := weekdayField("weekly", ptrext.Of(attunev1.UpsertDigestSubscriptionRequest{}))
		require.Error(t, err)
		require.Contains(t, err.Error(), "byweekday is required")
	})

	t.Run("weekly valid byweekday", func(t *testing.T) {
		t.Parallel()
		got, err := weekdayField("weekly", ptrext.Of(attunev1.UpsertDigestSubscriptionRequest{
			Byweekday: ptrext.Of(int32(0)),
		}))
		require.NoError(t, err)
		require.NotNil(t, got)
		require.Equal(t, 0, ptrext.Indirect(got))
	})

	t.Run("weekly byweekday 6 valid", func(t *testing.T) {
		t.Parallel()
		got, err := weekdayField("weekly", ptrext.Of(attunev1.UpsertDigestSubscriptionRequest{
			Byweekday: ptrext.Of(int32(6)),
		}))
		require.NoError(t, err)
		require.NotNil(t, got)
		require.Equal(t, 6, ptrext.Indirect(got))
	})

	t.Run("weekly byweekday negative errors", func(t *testing.T) {
		t.Parallel()
		_, err := weekdayField("weekly", ptrext.Of(attunev1.UpsertDigestSubscriptionRequest{
			Byweekday: ptrext.Of(int32(-1)),
		}))
		require.Error(t, err)
		require.Contains(t, err.Error(), "byweekday must be between")
	})
}

// ---------------------------------------------------------------------------
// timezoneField (direct unit tests)
// ---------------------------------------------------------------------------

func TestTimezoneField(t *testing.T) {
	t.Parallel()

	t.Run("nil timezone returns nil", func(t *testing.T) {
		t.Parallel()
		got, err := timezoneField(ptrext.Of(attunev1.UpsertDigestSubscriptionRequest{}))
		require.NoError(t, err)
		require.Nil(t, got)
	})

	t.Run("empty string timezone returns nil", func(t *testing.T) {
		t.Parallel()
		got, err := timezoneField(ptrext.Of(attunev1.UpsertDigestSubscriptionRequest{
			Timezone: ptrext.Of(""),
		}))
		require.NoError(t, err)
		require.Nil(t, got)
	})

	t.Run("whitespace-only timezone returns nil", func(t *testing.T) {
		t.Parallel()
		got, err := timezoneField(ptrext.Of(attunev1.UpsertDigestSubscriptionRequest{
			Timezone: ptrext.Of("   "),
		}))
		require.NoError(t, err)
		require.Nil(t, got)
	})

	t.Run("valid IANA timezone", func(t *testing.T) {
		t.Parallel()
		got, err := timezoneField(ptrext.Of(attunev1.UpsertDigestSubscriptionRequest{
			Timezone: ptrext.Of("America/New_York"),
		}))
		require.NoError(t, err)
		require.NotNil(t, got)
		require.Equal(t, "America/New_York", ptrext.Indirect(got))
	})

	t.Run("invalid timezone errors", func(t *testing.T) {
		t.Parallel()
		_, err := timezoneField(ptrext.Of(attunev1.UpsertDigestSubscriptionRequest{
			Timezone: ptrext.Of("Not/A/Timezone"),
		}))
		require.Error(t, err)
		require.Contains(t, err.Error(), "valid IANA name")
	})
}

// ---------------------------------------------------------------------------
// toDigestProto edge cases
// ---------------------------------------------------------------------------

func TestToDigestProtoEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("all optional fields nil", func(t *testing.T) {
		t.Parallel()
		now := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
		sub := digestsubrepo.Subscription{
			Enabled:        true,
			Frequency:      "daily",
			SendHour:       9,
			LLMMinFeedback: 6,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		out := toDigestProto(sub)
		require.True(t, out.GetEnabled())
		require.Equal(t, "daily", out.GetFrequency())
		require.Equal(t, int32(9), out.GetSendHour())
		require.Nil(t, out.Byweekday)
		require.Nil(t, out.Timezone)
		require.Nil(t, out.ThemePrompt)
		require.Nil(t, out.NextRunAt)
		require.Nil(t, out.LastRunAt)
	})

	t.Run("with LastRunAt set", func(t *testing.T) {
		t.Parallel()
		now := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
		lastRun := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
		sub := digestsubrepo.Subscription{
			Enabled:        true,
			Frequency:      "daily",
			SendHour:       9,
			LLMMinFeedback: 6,
			LastRunAt:      ptrext.Of(lastRun),
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		out := toDigestProto(sub)
		require.NotNil(t, out.LastRunAt)
		require.Equal(t, lastRun.UTC().Format(time.RFC3339), out.GetLastRunAt())
	})

	t.Run("with ThemePrompt set", func(t *testing.T) {
		t.Parallel()
		now := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
		sub := digestsubrepo.Subscription{
			Enabled:        true,
			Frequency:      "daily",
			SendHour:       9,
			LLMMinFeedback: 6,
			ThemePrompt:    ptrext.Of("weekly highlights"),
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		out := toDigestProto(sub)
		require.NotNil(t, out.ThemePrompt)
		require.Equal(t, "weekly highlights", out.GetThemePrompt())
	})

	t.Run("without Byweekday", func(t *testing.T) {
		t.Parallel()
		now := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
		sub := digestsubrepo.Subscription{
			Enabled:        true,
			Frequency:      "daily",
			SendHour:       9,
			LLMMinFeedback: 6,
			Timezone:       ptrext.Of("UTC"),
			NextRunAt:      ptrext.Of(now),
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		out := toDigestProto(sub)
		require.Nil(t, out.Byweekday)
		require.NotNil(t, out.Timezone)
		require.NotNil(t, out.NextRunAt)
	})

	t.Run("SendOnEmpty propagated", func(t *testing.T) {
		t.Parallel()
		now := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
		sub := digestsubrepo.Subscription{
			Enabled:        false,
			Frequency:      "weekly",
			SendHour:       0,
			SendOnEmpty:    true,
			LLMMinFeedback: 10,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		out := toDigestProto(sub)
		require.True(t, out.GetSendOnEmpty())
		require.False(t, out.GetEnabled())
		require.Equal(t, int32(10), out.GetLlmMinFeedback())
	})
}

// ---------------------------------------------------------------------------
// normalizeUpsert additional edge cases
// ---------------------------------------------------------------------------

func TestNormalizeUpsertExtended(t *testing.T) {
	t.Parallel()

	t.Run("ThemePrompt set", func(t *testing.T) {
		t.Parallel()
		sub, err := normalizeUpsert(ptrext.Of(attunev1.UpsertDigestSubscriptionRequest{
			Enabled:     true,
			Frequency:   "daily",
			SendHour:    9,
			ThemePrompt: ptrext.Of("product feedback themes"),
		}))
		require.NoError(t, err)
		require.NotNil(t, sub.ThemePrompt)
		require.Equal(t, "product feedback themes", ptrext.Indirect(sub.ThemePrompt))
	})

	t.Run("ThemePrompt whitespace-only not set", func(t *testing.T) {
		t.Parallel()
		sub, err := normalizeUpsert(ptrext.Of(attunev1.UpsertDigestSubscriptionRequest{
			Enabled:     true,
			Frequency:   "daily",
			SendHour:    9,
			ThemePrompt: ptrext.Of("   "),
		}))
		require.NoError(t, err)
		require.Nil(t, sub.ThemePrompt)
	})

	t.Run("ThemePrompt nil not set", func(t *testing.T) {
		t.Parallel()
		sub, err := normalizeUpsert(ptrext.Of(attunev1.UpsertDigestSubscriptionRequest{
			Enabled:   true,
			Frequency: "daily",
			SendHour:  9,
		}))
		require.NoError(t, err)
		require.Nil(t, sub.ThemePrompt)
	})

	t.Run("SendOnEmpty true", func(t *testing.T) {
		t.Parallel()
		sub, err := normalizeUpsert(ptrext.Of(attunev1.UpsertDigestSubscriptionRequest{
			Enabled:     true,
			Frequency:   "daily",
			SendHour:    9,
			SendOnEmpty: true,
		}))
		require.NoError(t, err)
		require.True(t, sub.SendOnEmpty)
	})

	t.Run("negative send_hour rejected", func(t *testing.T) {
		t.Parallel()
		_, err := normalizeUpsert(ptrext.Of(attunev1.UpsertDigestSubscriptionRequest{
			Frequency: "daily",
			SendHour:  -1,
		}))
		require.Error(t, err)
		require.Contains(t, err.Error(), "send_hour must be between")
	})

	t.Run("llm_min_feedback zero defaults", func(t *testing.T) {
		t.Parallel()
		sub, err := normalizeUpsert(ptrext.Of(attunev1.UpsertDigestSubscriptionRequest{
			Enabled:   true,
			Frequency: "daily",
			SendHour:  8,
		}))
		require.NoError(t, err)
		require.Equal(t, defaultLLMMinFeedback, sub.LLMMinFeedback)
	})

	t.Run("valid timezone preserved", func(t *testing.T) {
		t.Parallel()
		sub, err := normalizeUpsert(ptrext.Of(attunev1.UpsertDigestSubscriptionRequest{
			Enabled:   true,
			Frequency: "daily",
			SendHour:  8,
			Timezone:  ptrext.Of("Europe/London"),
		}))
		require.NoError(t, err)
		require.NotNil(t, sub.Timezone)
		require.Equal(t, "Europe/London", ptrext.Indirect(sub.Timezone))
	})
}

// ---------------------------------------------------------------------------
// resolveTimezone
// ---------------------------------------------------------------------------

func TestResolveTimezone(t *testing.T) {
	t.Parallel()

	t.Run("override provided", func(t *testing.T) {
		t.Parallel()
		h := NewHandler(
			ptrext.Of(configurableSubRepo{}),
			ptrext.Of(configurableTenantReader{
				t: ptrext.Of(tenant.Tenant{Timezone: "Asia/Shanghai"}),
			}),
		)
		got, err := h.resolveTimezone(context.Background(), "t1", ptrext.Of("America/Chicago"))
		require.NoError(t, err)
		require.Equal(t, "America/Chicago", got)
	})

	t.Run("empty override falls back to tenant", func(t *testing.T) {
		t.Parallel()
		h := NewHandler(
			ptrext.Of(configurableSubRepo{}),
			ptrext.Of(configurableTenantReader{
				t: ptrext.Of(tenant.Tenant{Timezone: "Europe/Berlin"}),
			}),
		)
		got, err := h.resolveTimezone(context.Background(), "t1", ptrext.Of(""))
		require.NoError(t, err)
		require.Equal(t, "Europe/Berlin", got)
	})

	t.Run("nil override falls back to tenant", func(t *testing.T) {
		t.Parallel()
		h := NewHandler(
			ptrext.Of(configurableSubRepo{}),
			ptrext.Of(configurableTenantReader{
				t: ptrext.Of(tenant.Tenant{Timezone: "US/Pacific"}),
			}),
		)
		got, err := h.resolveTimezone(context.Background(), "t1", nil)
		require.NoError(t, err)
		require.Equal(t, "US/Pacific", got)
	})

	t.Run("tenant lookup error propagated", func(t *testing.T) {
		t.Parallel()
		h := NewHandler(
			ptrext.Of(configurableSubRepo{}),
			ptrext.Of(configurableTenantReader{
				getErr: errors.New("db down"),
			}),
		)
		_, err := h.resolveTimezone(context.Background(), "t1", nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "db down")
	})
}

// ---------------------------------------------------------------------------
// Handler.Get
// ---------------------------------------------------------------------------

func TestHandlerGet(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		now := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
		h := NewHandler(
			ptrext.Of(configurableSubRepo{
				getSub: ptrext.Of(digestsubrepo.Subscription{
					Enabled:        true,
					Frequency:      "daily",
					SendHour:       9,
					LLMMinFeedback: 6,
					CreatedAt:      now,
					UpdatedAt:      now,
				}),
			}),
			ptrext.Of(configurableTenantReader{clustering: true}),
		)
		result, err := h.Get(coverageCtx(), ptrext.Of(attunev1.GetDigestSubscriptionRequest{}))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, result.Status)
		require.Equal(t, "daily", result.Body.GetFrequency())
		require.True(t, result.Body.GetClusteringEnabled())
	})

	t.Run("not found returns 404", func(t *testing.T) {
		t.Parallel()
		h := NewHandler(
			ptrext.Of(configurableSubRepo{getErr: digestsubrepo.ErrNotFound}),
			ptrext.Of(configurableTenantReader{}),
		)
		_, err := h.Get(coverageCtx(), ptrext.Of(attunev1.GetDigestSubscriptionRequest{}))
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, http.StatusNotFound, de.Status)
		require.Equal(t, attunev1.ErrorCode_NOT_FOUND, de.Code)
	})

	t.Run("repo error returns 500", func(t *testing.T) {
		t.Parallel()
		h := NewHandler(
			ptrext.Of(configurableSubRepo{getErr: errors.New("connection refused")}),
			ptrext.Of(configurableTenantReader{}),
		)
		_, err := h.Get(coverageCtx(), ptrext.Of(attunev1.GetDigestSubscriptionRequest{}))
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, http.StatusInternalServerError, de.Status)
		require.Equal(t, attunev1.ErrorCode_INTERNAL, de.Code)
	})

	t.Run("clustering error non-fatal", func(t *testing.T) {
		t.Parallel()
		now := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
		h := NewHandler(
			ptrext.Of(configurableSubRepo{
				getSub: ptrext.Of(digestsubrepo.Subscription{
					Enabled:        true,
					Frequency:      "daily",
					SendHour:       9,
					LLMMinFeedback: 6,
					CreatedAt:      now,
					UpdatedAt:      now,
				}),
			}),
			ptrext.Of(configurableTenantReader{clusterErr: errors.New("cluster query failed")}),
		)
		result, err := h.Get(coverageCtx(), ptrext.Of(attunev1.GetDigestSubscriptionRequest{}))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, result.Status)
		require.False(t, result.Body.GetClusteringEnabled())
	})
}

// ---------------------------------------------------------------------------
// Handler.Delete
// ---------------------------------------------------------------------------

func TestHandlerDelete(t *testing.T) {
	t.Parallel()

	t.Run("success returns NoContent", func(t *testing.T) {
		t.Parallel()
		h := NewHandler(
			ptrext.Of(configurableSubRepo{deleteErr: nil}),
			ptrext.Of(configurableTenantReader{}),
		)
		result, err := h.Delete(coverageCtx(), ptrext.Of(attunev1.DeleteDigestSubscriptionRequest{}))
		require.NoError(t, err)
		require.Equal(t, http.StatusNoContent, result.Status)
	})

	t.Run("not found returns 404", func(t *testing.T) {
		t.Parallel()
		h := NewHandler(
			ptrext.Of(configurableSubRepo{deleteErr: digestsubrepo.ErrNotFound}),
			ptrext.Of(configurableTenantReader{}),
		)
		_, err := h.Delete(coverageCtx(), ptrext.Of(attunev1.DeleteDigestSubscriptionRequest{}))
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, http.StatusNotFound, de.Status)
		require.Equal(t, attunev1.ErrorCode_NOT_FOUND, de.Code)
	})

	t.Run("repo error returns 500", func(t *testing.T) {
		t.Parallel()
		h := NewHandler(
			ptrext.Of(configurableSubRepo{deleteErr: errors.New("disk full")}),
			ptrext.Of(configurableTenantReader{}),
		)
		_, err := h.Delete(coverageCtx(), ptrext.Of(attunev1.DeleteDigestSubscriptionRequest{}))
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, http.StatusInternalServerError, de.Status)
		require.Equal(t, attunev1.ErrorCode_INTERNAL, de.Code)
	})
}

// ---------------------------------------------------------------------------
// Handler.Upsert (validation failure path)
// ---------------------------------------------------------------------------

func TestHandlerUpsertValidation(t *testing.T) {
	t.Parallel()

	t.Run("invalid frequency returns 400", func(t *testing.T) {
		t.Parallel()
		h := NewHandler(
			ptrext.Of(configurableSubRepo{}),
			ptrext.Of(configurableTenantReader{
				t: ptrext.Of(tenant.Tenant{Timezone: "UTC"}),
			}),
		)
		_, err := h.Upsert(coverageCtx(), ptrext.Of(attunev1.UpsertDigestSubscriptionRequest{
			Frequency: "hourly",
			SendHour:  9,
		}))
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, http.StatusBadRequest, de.Status)
		require.Equal(t, attunev1.ErrorCode_VALIDATION, de.Code)
	})

	t.Run("resolve timezone error returns 500", func(t *testing.T) {
		t.Parallel()
		h := NewHandler(
			ptrext.Of(configurableSubRepo{}),
			ptrext.Of(configurableTenantReader{
				getErr: errors.New("tenant store down"),
			}),
		)
		_, err := h.Upsert(coverageCtx(), ptrext.Of(attunev1.UpsertDigestSubscriptionRequest{
			Enabled:   true,
			Frequency: "daily",
			SendHour:  9,
		}))
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, http.StatusInternalServerError, de.Status)
		require.Equal(t, attunev1.ErrorCode_INTERNAL, de.Code)
	})

	t.Run("upsert repo error returns 500", func(t *testing.T) {
		t.Parallel()
		h := NewHandler(
			ptrext.Of(configurableSubRepo{upsertErr: errors.New("constraint violation")}),
			ptrext.Of(configurableTenantReader{
				t: ptrext.Of(tenant.Tenant{Timezone: "UTC"}),
			}),
		)
		_, err := h.Upsert(coverageCtx(), ptrext.Of(attunev1.UpsertDigestSubscriptionRequest{
			Enabled:   true,
			Frequency: "daily",
			SendHour:  9,
		}))
		var de *dispatcher.Error
		require.True(t, errors.As(err, &de))
		require.Equal(t, http.StatusInternalServerError, de.Status)
		require.Equal(t, attunev1.ErrorCode_INTERNAL, de.Code)
	})

	t.Run("success with override timezone", func(t *testing.T) {
		t.Parallel()
		now := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
		h := NewHandler(
			ptrext.Of(configurableSubRepo{
				upsertSub: ptrext.Of(digestsubrepo.Subscription{
					Enabled:        true,
					Frequency:      "daily",
					SendHour:       9,
					LLMMinFeedback: 6,
					Timezone:       ptrext.Of("America/New_York"),
					CreatedAt:      now,
					UpdatedAt:      now,
				}),
			}),
			ptrext.Of(configurableTenantReader{
				t: ptrext.Of(tenant.Tenant{Timezone: "UTC"}),
			}),
		)
		result, err := h.Upsert(coverageCtx(), ptrext.Of(attunev1.UpsertDigestSubscriptionRequest{
			Enabled:   true,
			Frequency: "daily",
			SendHour:  9,
			Timezone:  ptrext.Of("America/New_York"),
		}))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, result.Status)
		require.Equal(t, "daily", result.Body.GetFrequency())
	})
}
