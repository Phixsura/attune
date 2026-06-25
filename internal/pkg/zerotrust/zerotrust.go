// SPDX-License-Identifier: Apache-2.0

// Package zerotrust provides zero-trust security primitives.
package zerotrust

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// Principal represents an authenticated entity.
type Principal struct {
	ID         string
	Type       PrincipalType
	Attributes map[string]string
	AuthTime   time.Time
	ExpiresAt  time.Time
}

// PrincipalType represents the type of principal.
type PrincipalType string

const (
	PrincipalUser    PrincipalType = "user"
	PrincipalService PrincipalType = "service"
	PrincipalDevice  PrincipalType = "device"
)

// IsExpired returns whether the principal's credentials have expired.
func (p Principal) IsExpired() bool {
	return time.Now().After(p.ExpiresAt)
}

// HasAttribute checks if the principal has an attribute with the given value.
func (p Principal) HasAttribute(key, value string) bool {
	v, ok := p.Attributes[key]
	return ok && v == value
}

// Resource represents a protected resource.
type Resource struct {
	Type       string
	ID         string
	Owner      string
	Attributes map[string]string
}

// Action represents an action on a resource.
type Action string

const (
	ActionRead   Action = "read"
	ActionWrite  Action = "write"
	ActionDelete Action = "delete"
	ActionAdmin  Action = "admin"
)

// Decision represents an authorization decision.
type Decision string

const (
	DecisionAllow Decision = "allow"
	DecisionDeny  Decision = "deny"
)

// Policy represents an authorization policy.
type Policy struct {
	Name        string
	Description string
	Subjects    []SubjectMatch
	Resources   []ResourceMatch
	Actions     []Action
	Effect      Decision
	Conditions  []Condition
}

// SubjectMatch defines criteria for matching principals.
type SubjectMatch struct {
	Type       PrincipalType
	IDPattern  string            // Glob pattern
	Attributes map[string]string // Required attributes
}

// ResourceMatch defines criteria for matching resources.
type ResourceMatch struct {
	Type       string
	IDPattern  string
	Attributes map[string]string
}

// Condition represents a policy condition.
type Condition struct {
	Type  ConditionType
	Value string
}

// ConditionType represents types of conditions.
type ConditionType string

const (
	ConditionIPRange   ConditionType = "ip_range"
	ConditionTimeRange ConditionType = "time_range"
	ConditionMFA       ConditionType = "mfa"
	ConditionDevice    ConditionType = "device_trust"
)

// Authorizer evaluates authorization decisions.
type Authorizer struct {
	mu       sync.RWMutex
	policies []Policy
}

// NewAuthorizer creates a new authorizer.
func NewAuthorizer() *Authorizer {
	return ptrext.Of(Authorizer{
		policies: make([]Policy, 0),
	})
}

// AddPolicy adds a policy to the authorizer.
func (a *Authorizer) AddPolicy(p Policy) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.policies = append(a.policies, p)
}

// Request represents an authorization request.
type Request struct {
	Principal Principal
	Resource  Resource
	Action    Action
	Context   RequestContext
}

// RequestContext contains contextual information for authorization.
type RequestContext struct {
	IP        string
	UserAgent string
	MFAUsed   bool
	DeviceID  string
	Timestamp time.Time
}

// Authorize evaluates an authorization request.
func (a *Authorizer) Authorize(req Request) (Decision, string) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// Check if principal is expired
	if req.Principal.IsExpired() {
		return DecisionDeny, "principal expired"
	}

	// Evaluate policies (first match wins)
	for _, policy := range a.policies {
		if a.matchesPolicy(req, policy) {
			return policy.Effect, policy.Name
		}
	}

	// Default deny
	return DecisionDeny, "no matching policy"
}

func (a *Authorizer) matchesPolicy(req Request, policy Policy) bool {
	// Check subjects
	subjectMatches := len(policy.Subjects) == 0
	for _, s := range policy.Subjects {
		if a.matchesSubject(req.Principal, s) {
			subjectMatches = true
			break
		}
	}
	if !subjectMatches {
		return false
	}

	// Check resources
	resourceMatches := len(policy.Resources) == 0
	for _, r := range policy.Resources {
		if a.matchesResource(req.Resource, r) {
			resourceMatches = true
			break
		}
	}
	if !resourceMatches {
		return false
	}

	// Check action
	actionMatches := len(policy.Actions) == 0
	for _, action := range policy.Actions {
		if action == req.Action {
			actionMatches = true
			break
		}
	}
	if !actionMatches {
		return false
	}

	// Check conditions
	for _, cond := range policy.Conditions {
		if !a.checkCondition(req, cond) {
			return false
		}
	}

	return true
}

func (a *Authorizer) matchesSubject(p Principal, s SubjectMatch) bool {
	if s.Type != "" && s.Type != p.Type {
		return false
	}
	if s.IDPattern != "" && !matchGlob(s.IDPattern, p.ID) {
		return false
	}
	for k, v := range s.Attributes {
		if !p.HasAttribute(k, v) {
			return false
		}
	}
	return true
}

func (a *Authorizer) matchesResource(r Resource, m ResourceMatch) bool {
	if m.Type != "" && m.Type != r.Type {
		return false
	}
	if m.IDPattern != "" && !matchGlob(m.IDPattern, r.ID) {
		return false
	}
	for k, v := range m.Attributes {
		if rv, ok := r.Attributes[k]; !ok || rv != v {
			return false
		}
	}
	return true
}

func (a *Authorizer) checkCondition(req Request, cond Condition) bool {
	switch cond.Type {
	case ConditionMFA:
		return req.Context.MFAUsed
	case ConditionDevice:
		return req.Context.DeviceID != ""
	default:
		return true
	}
}

// matchGlob performs simple glob matching (* = any).
func matchGlob(pattern, value string) bool {
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := pattern[:len(pattern)-1]
		return strings.HasPrefix(value, prefix)
	}
	if strings.HasPrefix(pattern, "*") {
		suffix := pattern[1:]
		return strings.HasSuffix(value, suffix)
	}
	return pattern == value
}

// Middleware returns an HTTP middleware that enforces zero-trust authorization.
func (a *Authorizer) Middleware(resourceType string, action Action) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal := PrincipalFromContext(r.Context())
			if principal == nil {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			req := Request{
				Principal: ptrext.Indirect(principal),
				Resource: Resource{
					Type: resourceType,
					ID:   r.URL.Path,
				},
				Action: action,
				Context: RequestContext{
					IP:        r.RemoteAddr,
					UserAgent: r.UserAgent(),
					Timestamp: time.Now(),
				},
			}

			decision, reason := a.Authorize(req)
			if decision != DecisionAllow {
				http.Error(w, "Forbidden: "+reason, http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// contextKey is the context key for principals.
type contextKey struct{}

// WithPrincipal adds a principal to the context.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, contextKey{}, p)
}

// PrincipalFromContext retrieves the principal from context.
func PrincipalFromContext(ctx context.Context) *Principal {
	if v := ctx.Value(contextKey{}); v != nil {
		if p, ok := v.(*Principal); ok {
			return p
		}
	}
	return nil
}

// DeviceFingerprint generates a device fingerprint from request headers.
func DeviceFingerprint(r *http.Request) string {
	data := r.UserAgent() + r.Header.Get("Accept-Language") + r.Header.Get("Accept-Encoding")
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:8])
}

// RequestID generates a unique request ID.
func RequestID() string {
	hash := sha256.Sum256([]byte(time.Now().String()))
	return hex.EncodeToString(hash[:8])
}
