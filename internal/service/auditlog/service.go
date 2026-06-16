// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	auditlogrepo "github.com/Phixsura/attune/internal/repo/auditlog"
)

type writerRepo interface {
	Insert(ctx context.Context, entry auditlogrepo.Entry) error
	List(ctx context.Context, filter auditlogrepo.ListFilter) (auditlogrepo.ListResult, error)
	PruneBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

type Service struct {
	repo writerRepo
}

func New(repo writerRepo) *Service {
	return ptrext.Of(Service{repo: repo})
}

type Actor struct {
	Type      string
	ID        string
	Email     string
	IP        string
	UserAgent string
}

type Event struct {
	TenantID   string
	Actor      Actor
	Action     string
	TargetType string
	TargetID   string
	Summary    string
	Before     any
	After      any
}

type ListFilter struct {
	TenantID   string
	Actions    []string
	ActorType  string
	ActorID    string
	TargetType string
	TargetID   string
	From       *time.Time
	To         *time.Time
	Limit      int
	Cursor     string
	Unbounded  bool
}

func (s *Service) Record(ctx context.Context, event Event) error {
	beforeJSON, err := marshalJSON(event.Before)
	if err != nil {
		return fmt.Errorf("marshal before: %w", err)
	}
	afterJSON, err := marshalJSON(event.After)
	if err != nil {
		return fmt.Errorf("marshal after: %w", err)
	}
	entry := auditlogrepo.Entry{
		TenantID:       strings.TrimSpace(event.TenantID),
		ActorType:      strings.TrimSpace(event.Actor.Type),
		ActorID:        strings.TrimSpace(event.Actor.ID),
		ActorEmail:     strings.TrimSpace(event.Actor.Email),
		ActorIP:        strings.TrimSpace(event.Actor.IP),
		ActorUserAgent: strings.TrimSpace(event.Actor.UserAgent),
		Action:         strings.TrimSpace(event.Action),
		TargetType:     strings.TrimSpace(event.TargetType),
		TargetID:       strings.TrimSpace(event.TargetID),
		Summary:        strings.TrimSpace(event.Summary),
		BeforeJSON:     beforeJSON,
		AfterJSON:      afterJSON,
	}
	if entry.TenantID == "" || entry.ActorType == "" || entry.ActorID == "" || entry.Action == "" || entry.TargetType == "" {
		return fmt.Errorf("audit log event is missing required fields")
	}
	if !isKnownAction(entry.Action) {
		return fmt.Errorf("audit log event has unsupported action %q", entry.Action)
	}
	if err := s.repo.Insert(ctx, entry); err != nil {
		return err
	}
	metrics.AuditRowsWrittenTotal.WithLabelValues(entry.Action).Inc()
	return nil
}

func (s *Service) List(ctx context.Context, filter ListFilter) (auditlogrepo.ListResult, error) {
	return s.repo.List(ctx, auditlogrepo.ListFilter{
		TenantID:   strings.TrimSpace(filter.TenantID),
		Actions:    trimNonEmpty(filter.Actions),
		ActorType:  strings.TrimSpace(filter.ActorType),
		ActorID:    strings.TrimSpace(filter.ActorID),
		TargetType: strings.TrimSpace(filter.TargetType),
		TargetID:   strings.TrimSpace(filter.TargetID),
		From:       filter.From,
		To:         filter.To,
		Limit:      filter.Limit,
		Cursor:     strings.TrimSpace(filter.Cursor),
		Unbounded:  filter.Unbounded,
	})
}

func trimNonEmpty(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func (s *Service) Prune(ctx context.Context, retention time.Duration) (int64, error) {
	start := time.Now()
	cutoff := time.Now().Add(-retention)
	rows, err := s.repo.PruneBefore(ctx, cutoff)
	metrics.AuditPruneDurationSeconds.Observe(time.Since(start).Seconds())
	if err != nil {
		return 0, err
	}
	metrics.AuditRowsPrunedTotal.Add(float64(rows))
	return rows, nil
}

func ActorFromRequest(actorType, actorID string, req *http.Request) Actor {
	actor := Actor{
		Type: strings.TrimSpace(actorType),
		ID:   strings.TrimSpace(actorID),
	}
	if req == nil {
		return actor
	}
	actor.UserAgent = strings.TrimSpace(req.UserAgent())
	actor.IP = remoteAddrIP(req.RemoteAddr)
	return actor
}

func marshalJSON(value any) (json.RawMessage, error) {
	if value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case json.RawMessage:
		if len(typed) == 0 {
			return nil, nil
		}
		return typed, nil
	case []byte:
		if len(typed) == 0 {
			return nil, nil
		}
		return json.RawMessage(typed), nil
	default:
		b, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		return b, nil
	}
}

func remoteAddrIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err == nil {
		return host
	}
	return strings.TrimSpace(remoteAddr)
}
