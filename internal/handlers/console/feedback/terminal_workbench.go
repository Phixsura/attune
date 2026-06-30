package feedback

import (
	"net/http"
	"time"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	feedbackrepo "github.com/Phixsura/attune/internal/repo/feedback"
)

// GetTerminalFailureWorkbench handles GET /fb/v1/console/feedback/terminal-failures.
// The window follows the same current-month UTC convention as the generic stats view.
func (h *FeedbackHandler) GetTerminalFailureWorkbench(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	_ *attunev1.GetTerminalFailureWorkbenchRequest,
) (dispatcher.Result[*attunev1.GetTerminalFailureWorkbenchResponse], error) {
	const where = "console.FeedbackHandler.GetTerminalFailureWorkbench"
	auth := ctx.Auth
	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	workbench, err := h.repo.TerminalFailureWorkbench(ctx, auth.TenantID, from, now)
	if err != nil {
		logext.Errorf(ctx, "[%s] workbench query failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.GetTerminalFailureWorkbenchResponse](
			http.StatusInternalServerError,
			attunev1.ErrorCode_INTERNAL,
			"failed to read terminal failure workbench",
		)
	}
	if workbench == nil {
		return dispatcher.Fail[*attunev1.GetTerminalFailureWorkbenchResponse](
			http.StatusInternalServerError,
			attunev1.ErrorCode_INTERNAL,
			"terminal failure workbench unavailable",
		)
	}

	resp := ptrext.Of(attunev1.GetTerminalFailureWorkbenchResponse{
		PeriodStart:               workbench.PeriodStart.UTC().Format(time.RFC3339),
		PeriodEnd:                 workbench.PeriodEnd.UTC().Format(time.RFC3339),
		TotalTerminalFailures:     workbench.TotalTerminalFailures,
		ReasonClassClusters:       terminalFailureClusterListToProto(workbench.ReasonClassClusters),
		ModelChannelClusters:      terminalFailureClusterListToProto(workbench.ModelChannelClusters),
		ConfigFingerprintClusters: terminalFailureClusterListToProto(workbench.ConfigFingerprintClusters),
		AgeBucketClusters:         terminalFailureClusterListToProto(workbench.AgeBucketClusters),
	})
	if workbench.OldestCreatedAt != nil {
		resp.OldestCreatedAt = ptrext.Of(workbench.OldestCreatedAt.UTC().Format(time.RFC3339))
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,total:%d,reason_clusters:%d,model_clusters:%d",
		where, auth.TenantID, workbench.TotalTerminalFailures, len(resp.ReasonClassClusters), len(resp.ModelChannelClusters))
	return dispatcher.OK(resp)
}

func terminalFailureClusterListToProto(clusters []feedbackrepo.TerminalFailureCluster) []*attunev1.TerminalFailureCluster {
	if len(clusters) == 0 {
		return nil
	}
	out := make([]*attunev1.TerminalFailureCluster, 0, len(clusters))
	for _, cluster := range clusters {
		out = append(out, ptrext.Of(attunev1.TerminalFailureCluster{
			Key:               cluster.Key,
			Label:             cluster.Label,
			Count:             cluster.Count,
			OldestCreatedAt:   cluster.OldestCreatedAt.UTC().Format(time.RFC3339),
			NewestCreatedAt:   cluster.NewestCreatedAt.UTC().Format(time.RFC3339),
			SampleFeedbackIds: cluster.SampleFeedbackIDs,
			RemediationHint:   nullableString(cluster.RemediationHint),
		}))
	}
	return out
}
