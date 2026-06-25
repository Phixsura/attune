// SPDX-License-Identifier: Apache-2.0

package zerotrust

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPrincipal_IsExpired(t *testing.T) {
	t.Parallel()

	t.Run("not expired", func(t *testing.T) {
		p := Principal{
			ID:        "user1",
			ExpiresAt: time.Now().Add(time.Hour),
		}
		require.False(t, p.IsExpired())
	})

	t.Run("expired", func(t *testing.T) {
		p := Principal{
			ID:        "user1",
			ExpiresAt: time.Now().Add(-time.Hour),
		}
		require.True(t, p.IsExpired())
	})
}

func TestPrincipal_HasAttribute(t *testing.T) {
	t.Parallel()
	p := Principal{
		ID: "user1",
		Attributes: map[string]string{
			"role": "admin",
			"org":  "acme",
		},
	}

	require.True(t, p.HasAttribute("role", "admin"))
	require.False(t, p.HasAttribute("role", "user"))
	require.False(t, p.HasAttribute("missing", "value"))
}

func TestAuthorizer_AddPolicy(t *testing.T) {
	t.Parallel()
	a := NewAuthorizer()

	a.AddPolicy(Policy{
		Name:   "allow-read",
		Effect: DecisionAllow,
	})

	// Policy added successfully
	require.NotNil(t, a)
}

func TestAuthorizer_Authorize_DefaultDeny(t *testing.T) {
	t.Parallel()
	a := NewAuthorizer()

	req := Request{
		Principal: Principal{
			ID:        "user1",
			ExpiresAt: time.Now().Add(time.Hour),
		},
		Resource: Resource{Type: "document", ID: "123"},
		Action:   ActionRead,
	}

	decision, _ := a.Authorize(req)
	require.Equal(t, DecisionDeny, decision)
}

func TestAuthorizer_Authorize_ExpiredPrincipal(t *testing.T) {
	t.Parallel()
	a := NewAuthorizer()
	a.AddPolicy(Policy{
		Name:   "allow-all",
		Effect: DecisionAllow,
	})

	req := Request{
		Principal: Principal{
			ID:        "user1",
			ExpiresAt: time.Now().Add(-time.Hour), // Expired
		},
		Resource: Resource{Type: "document"},
		Action:   ActionRead,
	}

	decision, reason := a.Authorize(req)
	require.Equal(t, DecisionDeny, decision)
	require.Equal(t, "principal expired", reason)
}

func TestAuthorizer_Authorize_PolicyMatch(t *testing.T) {
	t.Parallel()
	a := NewAuthorizer()
	a.AddPolicy(Policy{
		Name: "allow-user-read",
		Subjects: []SubjectMatch{
			{Type: PrincipalUser},
		},
		Resources: []ResourceMatch{
			{Type: "document"},
		},
		Actions: []Action{ActionRead},
		Effect:  DecisionAllow,
	})

	req := Request{
		Principal: Principal{
			ID:        "user1",
			Type:      PrincipalUser,
			ExpiresAt: time.Now().Add(time.Hour),
		},
		Resource: Resource{Type: "document", ID: "123"},
		Action:   ActionRead,
	}

	decision, _ := a.Authorize(req)
	require.Equal(t, DecisionAllow, decision)
}

func TestAuthorizer_Authorize_ActionMismatch(t *testing.T) {
	t.Parallel()
	a := NewAuthorizer()
	a.AddPolicy(Policy{
		Name:    "allow-read-only",
		Actions: []Action{ActionRead},
		Effect:  DecisionAllow,
	})

	req := Request{
		Principal: Principal{
			ID:        "user1",
			ExpiresAt: time.Now().Add(time.Hour),
		},
		Resource: Resource{Type: "document"},
		Action:   ActionWrite, // Not allowed
	}

	decision, _ := a.Authorize(req)
	require.Equal(t, DecisionDeny, decision)
}

func TestAuthorizer_Authorize_AttributeMatch(t *testing.T) {
	t.Parallel()
	a := NewAuthorizer()
	a.AddPolicy(Policy{
		Name: "allow-admin",
		Subjects: []SubjectMatch{
			{Attributes: map[string]string{"role": "admin"}},
		},
		Effect: DecisionAllow,
	})

	t.Run("admin allowed", func(t *testing.T) {
		req := Request{
			Principal: Principal{
				ID:         "user1",
				Attributes: map[string]string{"role": "admin"},
				ExpiresAt:  time.Now().Add(time.Hour),
			},
			Action: ActionAdmin,
		}
		decision, _ := a.Authorize(req)
		require.Equal(t, DecisionAllow, decision)
	})

	t.Run("non-admin denied", func(t *testing.T) {
		req := Request{
			Principal: Principal{
				ID:         "user2",
				Attributes: map[string]string{"role": "user"},
				ExpiresAt:  time.Now().Add(time.Hour),
			},
			Action: ActionAdmin,
		}
		decision, _ := a.Authorize(req)
		require.Equal(t, DecisionDeny, decision)
	})
}

func TestAuthorizer_Authorize_MFACondition(t *testing.T) {
	t.Parallel()
	a := NewAuthorizer()
	a.AddPolicy(Policy{
		Name:       "require-mfa",
		Effect:     DecisionAllow,
		Conditions: []Condition{{Type: ConditionMFA}},
	})

	t.Run("with MFA", func(t *testing.T) {
		req := Request{
			Principal: Principal{ID: "user1", ExpiresAt: time.Now().Add(time.Hour)},
			Context:   RequestContext{MFAUsed: true},
		}
		decision, _ := a.Authorize(req)
		require.Equal(t, DecisionAllow, decision)
	})

	t.Run("without MFA", func(t *testing.T) {
		req := Request{
			Principal: Principal{ID: "user1", ExpiresAt: time.Now().Add(time.Hour)},
			Context:   RequestContext{MFAUsed: false},
		}
		decision, _ := a.Authorize(req)
		require.Equal(t, DecisionDeny, decision)
	})
}

func TestAuthorizer_Middleware(t *testing.T) {
	t.Parallel()
	a := NewAuthorizer()
	a.AddPolicy(Policy{
		Name:   "allow-all",
		Effect: DecisionAllow,
	})

	handler := a.Middleware("api", ActionRead)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("no principal", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		require.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("with principal", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		p := &Principal{ID: "user1", ExpiresAt: time.Now().Add(time.Hour)} // ptrext:allow test value
		req = req.WithContext(WithPrincipal(req.Context(), p))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
	})
}

func TestContext(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := &Principal{ID: "user1"} // ptrext:allow test value

	ctx = WithPrincipal(ctx, p)
	got := PrincipalFromContext(ctx)

	require.Equal(t, p, got)
}

func TestPrincipalFromContext_NotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	got := PrincipalFromContext(ctx)
	require.Nil(t, got)
}

func TestMatchGlob(t *testing.T) {
	t.Parallel()
	require.True(t, matchGlob("*", "anything"))
	require.True(t, matchGlob("user-*", "user-123"))
	require.True(t, matchGlob("*-admin", "super-admin"))
	require.True(t, matchGlob("exact", "exact"))
	require.False(t, matchGlob("exact", "different"))
}

func TestDeviceFingerprint(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept-Language", "en-US")

	fp := DeviceFingerprint(req)
	require.NotEmpty(t, fp)
	require.Len(t, fp, 16) // 8 bytes = 16 hex chars
}

func TestRequestID(t *testing.T) {
	t.Parallel()
	id1 := RequestID()
	id2 := RequestID()

	require.NotEmpty(t, id1)
	require.Len(t, id1, 16)
	// IDs should be different (time-based)
	require.NotEqual(t, id1, id2)
}

// ============================================================================
// Edge Cases
// ============================================================================

func TestPrincipal_ZeroExpiration(t *testing.T) {
	t.Parallel()
	p := Principal{
		ID:        "user1",
		ExpiresAt: time.Time{}, // Zero time
	}
	// Zero time is before now, so should be expired
	require.True(t, p.IsExpired())
}

func TestPrincipal_NilAttributes(t *testing.T) {
	t.Parallel()
	p := Principal{
		ID:         "user1",
		Attributes: nil,
	}
	require.False(t, p.HasAttribute("role", "admin"))
}

func TestPrincipal_EmptyAttributeValue(t *testing.T) {
	t.Parallel()
	p := Principal{
		ID:         "user1",
		Attributes: map[string]string{"role": ""},
	}
	require.True(t, p.HasAttribute("role", ""))
	require.False(t, p.HasAttribute("role", "admin"))
}

func TestAuthorizer_NoPolicies(t *testing.T) {
	t.Parallel()
	a := NewAuthorizer()

	req := Request{
		Principal: Principal{
			ID:        "user1",
			ExpiresAt: time.Now().Add(time.Hour),
		},
		Action: ActionRead,
	}

	decision, reason := a.Authorize(req)
	require.Equal(t, DecisionDeny, decision)
	require.Equal(t, "no matching policy", reason)
}

func TestAuthorizer_EmptyPrincipalID(t *testing.T) {
	t.Parallel()
	a := NewAuthorizer()
	a.AddPolicy(Policy{
		Name:   "allow-all",
		Effect: DecisionAllow,
	})

	req := Request{
		Principal: Principal{
			ID:        "", // Empty ID
			ExpiresAt: time.Now().Add(time.Hour),
		},
		Action: ActionRead,
	}

	decision, _ := a.Authorize(req)
	require.Equal(t, DecisionAllow, decision, "empty ID should still be allowed if policy matches")
}

func TestAuthorizer_MultipleMatchingPolicies_FirstWins(t *testing.T) {
	t.Parallel()
	a := NewAuthorizer()

	// First policy: deny
	a.AddPolicy(Policy{
		Name:   "deny-first",
		Effect: DecisionDeny,
	})
	// Second policy: allow
	a.AddPolicy(Policy{
		Name:   "allow-second",
		Effect: DecisionAllow,
	})

	req := Request{
		Principal: Principal{
			ID:        "user1",
			ExpiresAt: time.Now().Add(time.Hour),
		},
		Action: ActionRead,
	}

	decision, reason := a.Authorize(req)
	require.Equal(t, DecisionDeny, decision)
	require.Equal(t, "deny-first", reason)
}

func TestAuthorizer_ResourceIDGlobPattern(t *testing.T) {
	t.Parallel()
	a := NewAuthorizer()
	a.AddPolicy(Policy{
		Name: "allow-user-docs",
		Resources: []ResourceMatch{
			{Type: "document", IDPattern: "user-*"},
		},
		Effect: DecisionAllow,
	})

	t.Run("matching pattern", func(t *testing.T) {
		req := Request{
			Principal: Principal{ID: "user1", ExpiresAt: time.Now().Add(time.Hour)},
			Resource:  Resource{Type: "document", ID: "user-123"},
			Action:    ActionRead,
		}
		decision, _ := a.Authorize(req)
		require.Equal(t, DecisionAllow, decision)
	})

	t.Run("non-matching pattern", func(t *testing.T) {
		req := Request{
			Principal: Principal{ID: "user1", ExpiresAt: time.Now().Add(time.Hour)},
			Resource:  Resource{Type: "document", ID: "admin-123"},
			Action:    ActionRead,
		}
		decision, _ := a.Authorize(req)
		require.Equal(t, DecisionDeny, decision)
	})
}

func TestAuthorizer_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	a := NewAuthorizer()

	done := make(chan bool)

	// Concurrent policy additions
	go func() {
		for i := 0; i < 100; i++ {
			a.AddPolicy(Policy{
				Name:   "policy-" + string(rune('a'+i%26)),
				Effect: DecisionAllow,
			})
		}
		done <- true
	}()

	// Concurrent authorizations
	go func() {
		for i := 0; i < 100; i++ {
			req := Request{
				Principal: Principal{ID: "user1", ExpiresAt: time.Now().Add(time.Hour)},
				Action:    ActionRead,
			}
			a.Authorize(req)
		}
		done <- true
	}()

	for i := 0; i < 2; i++ {
		<-done
	}
}

func TestAuthorizer_AllActions(t *testing.T) {
	t.Parallel()
	a := NewAuthorizer()
	a.AddPolicy(Policy{
		Name:    "allow-read-write",
		Actions: []Action{ActionRead, ActionWrite},
		Effect:  DecisionAllow,
	})

	for _, action := range []Action{ActionRead, ActionWrite} {
		req := Request{
			Principal: Principal{ID: "user1", ExpiresAt: time.Now().Add(time.Hour)},
			Action:    action,
		}
		decision, _ := a.Authorize(req)
		require.Equal(t, DecisionAllow, decision)
	}

	// Delete and Admin should be denied
	for _, action := range []Action{ActionDelete, ActionAdmin} {
		req := Request{
			Principal: Principal{ID: "user1", ExpiresAt: time.Now().Add(time.Hour)},
			Action:    action,
		}
		decision, _ := a.Authorize(req)
		require.Equal(t, DecisionDeny, decision)
	}
}

func TestAuthorizer_DeviceCondition(t *testing.T) {
	t.Parallel()
	a := NewAuthorizer()
	a.AddPolicy(Policy{
		Name:       "require-device",
		Effect:     DecisionAllow,
		Conditions: []Condition{{Type: ConditionDevice}},
	})

	t.Run("with device", func(t *testing.T) {
		req := Request{
			Principal: Principal{ID: "user1", ExpiresAt: time.Now().Add(time.Hour)},
			Context:   RequestContext{DeviceID: "device-123"},
		}
		decision, _ := a.Authorize(req)
		require.Equal(t, DecisionAllow, decision)
	})

	t.Run("without device", func(t *testing.T) {
		req := Request{
			Principal: Principal{ID: "user1", ExpiresAt: time.Now().Add(time.Hour)},
			Context:   RequestContext{DeviceID: ""},
		}
		decision, _ := a.Authorize(req)
		require.Equal(t, DecisionDeny, decision)
	})
}

func TestMatchGlob_EdgeCases(t *testing.T) {
	t.Parallel()
	// Empty pattern
	require.False(t, matchGlob("", "anything"))
	require.True(t, matchGlob("", ""))

	// Empty value
	require.True(t, matchGlob("*", ""))

	// Only wildcard
	require.True(t, matchGlob("*", ""))
	require.True(t, matchGlob("*", "anything"))

	// Wildcards in middle (not supported, treated as literal)
	require.False(t, matchGlob("a*b", "aXb"))
}

func TestDeviceFingerprint_EmptyHeaders(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// No headers set

	fp := DeviceFingerprint(req)
	require.NotEmpty(t, fp)
	require.Len(t, fp, 16)
}

func TestDeviceFingerprint_Consistency(t *testing.T) {
	t.Parallel()
	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	req1.Header.Set("User-Agent", "Mozilla/5.0")

	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.Header.Set("User-Agent", "Mozilla/5.0")

	fp1 := DeviceFingerprint(req1)
	fp2 := DeviceFingerprint(req2)

	require.Equal(t, fp1, fp2, "same headers should produce same fingerprint")
}

func TestMiddleware_DeniedRequest(t *testing.T) {
	t.Parallel()
	a := NewAuthorizer()
	// No policies = deny all

	handler := a.Middleware("api", ActionWrite)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	p := &Principal{ID: "user1", ExpiresAt: time.Now().Add(time.Hour)} // ptrext:allow test value
	req = req.WithContext(WithPrincipal(req.Context(), p))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestPrincipalTypes(t *testing.T) {
	t.Parallel()
	require.Equal(t, PrincipalType("user"), PrincipalUser)
	require.Equal(t, PrincipalType("service"), PrincipalService)
	require.Equal(t, PrincipalType("device"), PrincipalDevice)
}

func TestDecisionTypes(t *testing.T) {
	t.Parallel()
	require.Equal(t, Decision("allow"), DecisionAllow)
	require.Equal(t, Decision("deny"), DecisionDeny)
}

func TestConditionTypes(t *testing.T) {
	t.Parallel()
	require.Equal(t, ConditionType("ip_range"), ConditionIPRange)
	require.Equal(t, ConditionType("time_range"), ConditionTimeRange)
	require.Equal(t, ConditionType("mfa"), ConditionMFA)
	require.Equal(t, ConditionType("device_trust"), ConditionDevice)
}
