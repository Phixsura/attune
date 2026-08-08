// lint-artifacts:file-allow localized workflow seed-state display names (zh/en)

package workflow

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/feedbackaudit"
	"github.com/Phixsura/attune/internal/repo/workflowstate"
)

var (
	ErrInvalidTransition = errors.New("transition not allowed")
	ErrStateNotFound     = errors.New("workflow state not found")
	ErrNoWorkflowState   = errors.New("feedback has no workflow state")
	ErrLastDefault       = errors.New("cannot remove last default state")
	ErrStateInUse        = errors.New("state has active feedback")
)

type StateStore interface {
	List(ctx context.Context, tenantID string, includeArchived bool) ([]workflowstate.WorkflowState, error)
	GetByTenantAndID(ctx context.Context, tenantID, id string) (*workflowstate.WorkflowState, error)
	Create(ctx context.Context, s workflowstate.WorkflowState) (*workflowstate.WorkflowState, error)
	Update(ctx context.Context, s workflowstate.WorkflowState) (*workflowstate.WorkflowState, error)
	Archive(ctx context.Context, tenantID, id string) error
	DeleteTransitionsForState(ctx context.Context, tenantID, stateID string) error
	CountActiveFeedback(ctx context.Context, tenantID, stateID string) (int64, error)
	CountActiveDefaults(ctx context.Context, tenantID string) (int64, error)
	CheckTransition(ctx context.Context, tenantID, fromID, toID string) (bool, error)
	CheckTransitionTx(ctx context.Context, tx pgx.Tx, tenantID, fromID, toID string) (bool, error)
	AllowedNext(ctx context.Context, tenantID, fromID string) ([]workflowstate.WorkflowState, error)
	ListTransitions(ctx context.Context, tenantID string) ([]workflowstate.Transition, error)
	ReplaceTransitions(ctx context.Context, tenantID string, edges []workflowstate.TransitionEdge) ([]workflowstate.Transition, error)
	UpsertStateReturningID(ctx context.Context, tx pgx.Tx, s workflowstate.WorkflowState) (string, error)
	InsertTransitionIgnoreConflict(ctx context.Context, tx pgx.Tx, tenantID, fromID, toID string) error
	GetCurrentStateForUpdate(ctx context.Context, tx pgx.Tx, tenantID string, feedbackID int64) (*string, error)
	ArchiveWithTransitions(ctx context.Context, tenantID, stateID string) error
	SetFeedbackState(ctx context.Context, tx pgx.Tx, feedbackID int64, stateID string) error
}

type AuditWriter interface {
	WriteTx(ctx context.Context, tx pgx.Tx, e feedbackaudit.Entry) error
	List(ctx context.Context, tenantID string, feedbackID int64, cursor string, limit int) ([]feedbackaudit.Entry, string, error)
}

type TransitionResult struct {
	FeedbackID int64
	FromState  *workflowstate.WorkflowState
	ToState    *workflowstate.WorkflowState
}

type BatchItemFailure struct {
	FeedbackID int64
	Code       string
	Message    string
}

type BatchResult struct {
	Succeeded int
	Failed    []BatchItemFailure
}

type SurveyTransitionEvent struct {
	TenantID          string
	FeedbackID        int64
	FromStateID       string
	FromStateName     string
	FromStateCategory string
	ToStateID         string
	ToStateName       string
	ToStateCategory   string
	ActorID           string
}

type SurveySink interface {
	RecordWorkflowTransition(ctx context.Context, event SurveyTransitionEvent) (int, error)
}

type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

type Service struct {
	states  StateStore
	audits  AuditWriter
	pool    TxBeginner
	surveys SurveySink
}

func NewService(states StateStore, audits AuditWriter, pool TxBeginner) *Service {
	return ptrext.Of(Service{states: states, audits: audits, pool: pool})
}

func (s *Service) SetSurveySink(sink SurveySink) {
	s.surveys = sink
}

func (s *Service) Transition(ctx context.Context, tenantID string, feedbackID int64, toStateID, byUser, comment string) (*TransitionResult, error) {
	const where = "service.workflow.Transition"

	toState, err := s.states.GetByTenantAndID(ctx, tenantID, toStateID)
	if err != nil {
		if errors.Is(err, workflowstate.ErrNotFound) {
			return nil, ErrStateNotFound
		}
		return nil, err
	}
	if toState.ArchivedAt != nil {
		return nil, ErrStateNotFound
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	currentID, err := s.states.GetCurrentStateForUpdate(ctx, tx, tenantID, feedbackID)
	if err != nil {
		if errors.Is(err, workflowstate.ErrNotFound) {
			return nil, ErrStateNotFound
		}
		return nil, err
	}

	if currentID == nil {
		metrics.WorkflowTransitionsTotal.WithLabelValues(tenantID, "error").Inc()
		return nil, ErrNoWorkflowState
	}

	fromState, err := s.states.GetByTenantAndID(ctx, tenantID, ptrext.Indirect(currentID))
	if err != nil {
		return nil, err
	}

	allowed, err := s.states.CheckTransitionTx(ctx, tx, tenantID, fromState.ID, toStateID)
	if err != nil {
		return nil, err
	}
	if !allowed {
		metrics.WorkflowTransitionsTotal.WithLabelValues(tenantID, "invalid").Inc()
		logext.Warnf(ctx, "[%s] invalid transition,tenant_id:%s,feedback_id:%d,from:%s,to:%s",
			where, tenantID, feedbackID, fromState.ID, toStateID)
		return nil, ErrInvalidTransition
	}

	if err := s.states.SetFeedbackState(ctx, tx, feedbackID, toStateID); err != nil {
		return nil, err
	}

	if err := s.audits.WriteTx(ctx, tx, feedbackaudit.Entry{
		TenantID:   tenantID,
		FeedbackID: feedbackID,
		EntityType: "workflow_state",
		FieldName:  "workflow_state_id",
		OldValue:   ptrext.Of(fromState.DisplayName.Resolve(nil)),
		NewValue:   ptrext.Of(toState.DisplayName.Resolve(nil)),
		Comment:    comment,
		ChangedBy:  byUser,
	}); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit transition: %w", err)
	}

	metrics.WorkflowTransitionsTotal.WithLabelValues(tenantID, "success").Inc()
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,feedback_id:%d,from:%s,to:%s,by:%s",
		where, tenantID, feedbackID, fromState.Name, toState.Name, byUser)

	s.recordSurveyTransition(ctx, tenantID, feedbackID, fromState, toState, byUser)

	return ptrext.Of(TransitionResult{
		FeedbackID: feedbackID,
		FromState:  fromState,
		ToState:    toState,
	}), nil
}

func (s *Service) BatchTransition(ctx context.Context, tenantID string, feedbackIDs []int64, toStateID, byUser, comment string) (*BatchResult, error) {
	const where = "service.workflow.BatchTransition"
	metrics.WorkflowBatchSize.Observe(float64(len(feedbackIDs)))

	result := ptrext.Of(BatchResult{})
	for _, fid := range feedbackIDs {
		_, err := s.Transition(ctx, tenantID, fid, toStateID, byUser, comment)
		if err != nil {
			code := "INTERNAL"
			msg := err.Error()
			switch {
			case errors.Is(err, ErrInvalidTransition):
				code = "INVALID_TRANSITION"
				msg = "transition not allowed"
			case errors.Is(err, ErrStateNotFound):
				code = "WORKFLOW_STATE_NOT_FOUND"
				msg = "target state not found"
			case errors.Is(err, ErrNoWorkflowState):
				code = "NO_WORKFLOW_STATE"
				msg = "feedback has no workflow state"
			}
			result.Failed = append(result.Failed, BatchItemFailure{
				FeedbackID: fid,
				Code:       code,
				Message:    msg,
			})
			logext.Warnf(ctx, "[%s] batch item failed,tenant_id:%s,feedback_id:%d,code:%s",
				where, tenantID, fid, code)
			continue
		}
		result.Succeeded++
	}
	return result, nil
}

func (s *Service) recordSurveyTransition(
	ctx context.Context,
	tenantID string,
	feedbackID int64,
	fromState *workflowstate.WorkflowState,
	toState *workflowstate.WorkflowState,
	actorID string,
) {
	const where = "service.workflow.recordSurveyTransition"
	if s.surveys == nil || fromState == nil || toState == nil {
		return
	}
	_, err := s.surveys.RecordWorkflowTransition(ctx, SurveyTransitionEvent{
		TenantID:          tenantID,
		FeedbackID:        feedbackID,
		FromStateID:       fromState.ID,
		FromStateName:     fromState.Name,
		FromStateCategory: fromState.Category,
		ToStateID:         toState.ID,
		ToStateName:       toState.Name,
		ToStateCategory:   toState.Category,
		ActorID:           actorID,
	})
	if err != nil {
		logext.Warnf(ctx, "[%s] survey trigger failed,tenant_id:%s,feedback_id:%d,err:%+v",
			where, tenantID, feedbackID, err.Error())
	}
}

func (s *Service) ArchiveState(ctx context.Context, tenantID, stateID string) error {
	st, err := s.states.GetByTenantAndID(ctx, tenantID, stateID)
	if err != nil {
		if errors.Is(err, workflowstate.ErrNotFound) {
			return ErrStateNotFound
		}
		return err
	}
	if st.ArchivedAt != nil {
		return ErrStateNotFound
	}

	count, err := s.states.CountActiveFeedback(ctx, tenantID, stateID)
	if err != nil {
		return err
	}
	if count > 0 {
		return ErrStateInUse
	}

	if st.IsDefault {
		defaults, err := s.states.CountActiveDefaults(ctx, tenantID)
		if err != nil {
			return err
		}
		if defaults <= 1 {
			return ErrLastDefault
		}
	}

	return s.states.ArchiveWithTransitions(ctx, tenantID, stateID)
}

type seedState struct {
	Name        string // stable machine key (slug)
	DisplayName domain.I18nString
	Color       string
	Category    string
	Position    int
	IsDefault   bool
}

type seedTransition struct {
	FromName string
	ToName   string
}

func (s *Service) SeedDefaults(ctx context.Context, tenantID string) error {
	const where = "service.workflow.SeedDefaults"

	states := []seedState{
		{Name: "pending", DisplayName: domain.I18nString{"default": "Pending", "en": "Pending", "zh": "待处理"}, Color: "#6b7280", Category: "open", Position: 0, IsDefault: true},
		{Name: "triaged", DisplayName: domain.I18nString{"default": "Triaged", "en": "Triaged", "zh": "已分拣"}, Color: "#3b82f6", Category: "open", Position: 1},
		{Name: "in_progress", DisplayName: domain.I18nString{"default": "In progress", "en": "In progress", "zh": "处理中"}, Color: "#f59e0b", Category: "active", Position: 2},
		{Name: "fixed", DisplayName: domain.I18nString{"default": "Fixed", "en": "Fixed", "zh": "已修复"}, Color: "#10b981", Category: "closed", Position: 3},
		{Name: "wont_fix", DisplayName: domain.I18nString{"default": "Won't fix", "en": "Won't fix", "zh": "不处理"}, Color: "#ef4444", Category: "closed", Position: 4},
	}
	transitions := []seedTransition{
		{"pending", "triaged"},
		{"pending", "in_progress"},
		{"pending", "wont_fix"},
		{"triaged", "in_progress"},
		{"triaged", "wont_fix"},
		{"in_progress", "fixed"},
		{"in_progress", "wont_fix"},
		{"in_progress", "pending"},
		{"fixed", "pending"},
		{"wont_fix", "pending"},
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("begin serializable tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	nameToID := make(map[string]string, len(states))
	for _, st := range states {
		id, err := s.states.UpsertStateReturningID(ctx, tx, workflowstate.WorkflowState{
			TenantID:    tenantID,
			Name:        st.Name,
			DisplayName: st.DisplayName,
			Color:       st.Color,
			Category:    st.Category,
			Position:    st.Position,
			IsDefault:   st.IsDefault,
		})
		if err != nil {
			return fmt.Errorf("seed state %q: %w", st.Name, err)
		}
		nameToID[st.Name] = id
	}

	for _, tr := range transitions {
		fromID := nameToID[tr.FromName]
		toID := nameToID[tr.ToName]
		if err := s.states.InsertTransitionIgnoreConflict(ctx, tx, tenantID, fromID, toID); err != nil {
			return fmt.Errorf("seed transition %s→%s: %w", tr.FromName, tr.ToName, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit seed: %w", err)
	}

	logext.Infof(ctx, "[%s] OK,tenant_id:%s,states:%d,transitions:%d",
		where, tenantID, len(states), len(transitions))
	return nil
}
