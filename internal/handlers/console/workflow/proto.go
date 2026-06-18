package workflow

import (
	"time"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/workflowstate"
)

// StateToProto maps a workflow state to its wire shape. It is the single
// source of this mapping — the feedback handler embeds workflow state on
// feedback rows and calls this too, so the conversion lives in one place.
func StateToProto(s workflowstate.WorkflowState) *attunev1.WorkflowState {
	return ptrext.Of(attunev1.WorkflowState{
		Id:          s.ID,
		Name:        s.Name,
		DisplayName: i18nToProto(s.DisplayName),
		Color:       s.Color,
		Category:    s.Category,
		Position:    int32(s.Position),
		IsDefault:   s.IsDefault,
		Archived:    s.ArchivedAt != nil,
		CreatedAt:   s.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   s.UpdatedAt.UTC().Format(time.RFC3339),
	})
}

// i18nToProto / i18nFromProto convert domain.I18nString to and from the proto
// wrapper. A second small copy of the enrichconfig helpers; per CLAUDE.md §6
// these get hoisted into one shared package once a third caller appears.
func i18nToProto(s domain.I18nString) *attunev1.I18NString {
	if len(s) == 0 {
		return ptrext.Of(attunev1.I18NString{Entries: map[string]string{}})
	}
	entries := make(map[string]string, len(s))
	for k, v := range s {
		entries[k] = v
	}
	return ptrext.Of(attunev1.I18NString{Entries: entries})
}

func i18nFromProto(in *attunev1.I18NString) domain.I18nString {
	if in == nil || len(in.GetEntries()) == 0 {
		return nil
	}
	out := make(domain.I18nString, len(in.GetEntries()))
	for k, v := range in.GetEntries() {
		out[k] = v
	}
	return out
}
