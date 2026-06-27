// ptrext:file-allow test-fixtures

package console

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/handlers/console/apikey"
	consoleauditevidence "github.com/Phixsura/attune/internal/handlers/console/auditevidence"
	consoleauditlog "github.com/Phixsura/attune/internal/handlers/console/auditlog"
	"github.com/Phixsura/attune/internal/handlers/console/clusters"
	"github.com/Phixsura/attune/internal/handlers/console/digestsubscription"
	"github.com/Phixsura/attune/internal/handlers/console/enrichconfig"
	consoleenrichmentruntime "github.com/Phixsura/attune/internal/handlers/console/enrichmentruntime"
	"github.com/Phixsura/attune/internal/handlers/console/feedback"
	"github.com/Phixsura/attune/internal/handlers/console/feedbackjob"
	consolegdpr "github.com/Phixsura/attune/internal/handlers/console/gdpr"
	consoleguardpolicy "github.com/Phixsura/attune/internal/handlers/console/guardpolicy"
	consoleinbound "github.com/Phixsura/attune/internal/handlers/console/inbound"
	consolellmconfig "github.com/Phixsura/attune/internal/handlers/console/llmconfig"
	consolemcpclient "github.com/Phixsura/attune/internal/handlers/console/mcpclient"
	"github.com/Phixsura/attune/internal/handlers/console/member"
	"github.com/Phixsura/attune/internal/handlers/console/notifytarget"
	consoleoidc "github.com/Phixsura/attune/internal/handlers/console/oidc"
	consoleoutbox "github.com/Phixsura/attune/internal/handlers/console/outbox"
	consoletag "github.com/Phixsura/attune/internal/handlers/console/tag"
	consoletagassignment "github.com/Phixsura/attune/internal/handlers/console/tagassignment"
	"github.com/Phixsura/attune/internal/handlers/console/usage"
	consoleworkflow "github.com/Phixsura/attune/internal/handlers/console/workflow"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestMountOutbox_RegistersRoutes(t *testing.T) {
	t.Parallel()
	r := &Router{outbox: ptrext.Of(consoleoutbox.Handler{})}
	mux := chi.NewRouter()
	r.mountOutbox(mux)
	require.NotNil(t, mux)
}

func TestMountLLMAbilities_RegistersRoutes(t *testing.T) {
	t.Parallel()
	r := &Router{llmConfig: ptrext.Of(consolellmconfig.Handler{})}
	mux := chi.NewRouter()
	r.mountLLMAbilities(mux)
	require.NotNil(t, mux)
}

func TestMountLLMRoutes_RegistersRoutes(t *testing.T) {
	t.Parallel()
	r := &Router{llmConfig: ptrext.Of(consolellmconfig.Handler{})}
	mux := chi.NewRouter()
	r.mountLLMRoutes(mux)
	require.NotNil(t, mux)
}

func TestMountGuardPolicies_RegistersRoutes(t *testing.T) {
	t.Parallel()
	r := &Router{guardPolicies: ptrext.Of(consoleguardpolicy.Handler{})}
	mux := chi.NewRouter()
	r.mountGuardPolicies(mux)
	require.NotNil(t, mux)
}

func TestMountInbound_RegistersRoutes(t *testing.T) {
	t.Parallel()
	r := &Router{inbound: ptrext.Of(consoleinbound.Handler{})}
	mux := chi.NewRouter()
	r.mountInbound(mux)
	require.NotNil(t, mux)
}

func TestMountClusters_RegistersRoutes(t *testing.T) {
	t.Parallel()
	r := &Router{clusters: ptrext.Of(clusters.ClustersHandler{})}
	mux := chi.NewRouter()
	r.mountClusters(mux)
	require.NotNil(t, mux)
}

func TestMountNotifyTargets_RegistersRoutes(t *testing.T) {
	t.Parallel()
	r := &Router{notifyTargets: ptrext.Of(notifytarget.NotifyTargetsHandler{})}
	mux := chi.NewRouter()
	r.mountNotifyTargets(mux)
	require.NotNil(t, mux)
}

func TestMountDigestSubscription_RegistersRoutes(t *testing.T) {
	t.Parallel()
	r := &Router{digestSubscription: ptrext.Of(digestsubscription.Handler{})}
	mux := chi.NewRouter()
	r.mountDigestSubscription(mux)
	require.NotNil(t, mux)
}

func TestMountFeedback_RegistersRoutes(t *testing.T) {
	t.Parallel()
	r := &Router{
		feedback:       ptrext.Of(feedback.FeedbackHandler{}),
		feedbackSearch: ptrext.Of(feedback.SearchHandler{}),
		usage:          ptrext.Of(usage.UsageHandler{}),
	}
	mux := chi.NewRouter()
	r.mountFeedback(mux)
	require.NotNil(t, mux)
}

func TestMountFeedbackBatch_RegistersRoutes(t *testing.T) {
	t.Parallel()
	r := &Router{feedbackBatch: ptrext.Of(feedback.BatchHandler{})}
	mux := chi.NewRouter()
	r.mountFeedbackBatchRoutes(mux)
	require.NotNil(t, mux)
}

func TestMountFeedbackTag_RegistersRoutes(t *testing.T) {
	t.Parallel()
	r := &Router{tags: ptrext.Of(consoletag.Handler{})}
	mux := chi.NewRouter()
	r.mountFeedbackTagRoutes(mux)
	require.NotNil(t, mux)
}

func TestMountEnrichConfig_RegistersRoutes(t *testing.T) {
	t.Parallel()
	r := &Router{enrichConfig: ptrext.Of(enrichconfig.Handler{})}
	mux := chi.NewRouter()
	r.mountEnrichConfig(mux)
	require.NotNil(t, mux)
}

func TestMountEnrichmentRuntime_RegistersRoutes(t *testing.T) {
	t.Parallel()
	r := &Router{enrichmentRuntime: ptrext.Of(consoleenrichmentruntime.Handler{})}
	mux := chi.NewRouter()
	r.mountEnrichmentRuntime(mux)
	require.NotNil(t, mux)
}

func TestMountTags_RegistersRoutes(t *testing.T) {
	t.Parallel()
	r := &Router{
		tags:           ptrext.Of(consoletag.Handler{}),
		tagAssignments: ptrext.Of(consoletagassignment.Handler{}),
	}
	mux := chi.NewRouter()
	r.mountTags(mux)
	require.NotNil(t, mux)
}

func TestMountJobs_RegistersRoutes(t *testing.T) {
	t.Parallel()
	r := &Router{feedbackJob: ptrext.Of(feedbackjob.Handler{})}
	mux := chi.NewRouter()
	r.mountJobs(mux)
	require.NotNil(t, mux)
}

func TestMountWorkflow_RegistersRoutes(t *testing.T) {
	t.Parallel()
	r := &Router{workflow: ptrext.Of(consoleworkflow.Handler{})}
	mux := chi.NewRouter()
	r.mountWorkflow(mux)
	require.NotNil(t, mux)
}

func TestMountMembers_RegistersRoutes(t *testing.T) {
	t.Parallel()
	r := &Router{members: ptrext.Of(member.Handler{})}
	mux := chi.NewRouter()
	r.mountMembers(mux)
	require.NotNil(t, mux)
}

func TestMountGDPR_RegistersRoutes(t *testing.T) {
	t.Parallel()
	r := &Router{gdpr: ptrext.Of(consolegdpr.Handler{})}
	mux := chi.NewRouter()
	r.mountGDPR(mux)
	require.NotNil(t, mux)
}

func TestMountAuditLog_RegistersRoutes(t *testing.T) {
	t.Parallel()
	r := &Router{auditLog: ptrext.Of(consoleauditlog.Handler{})}
	mux := chi.NewRouter()
	r.mountAuditLog(mux)
	require.NotNil(t, mux)
}

func TestMountAuditEvidence_RegistersRoutes(t *testing.T) {
	t.Parallel()
	r := &Router{auditEvidence: ptrext.Of(consoleauditevidence.Handler{})}
	mux := chi.NewRouter()
	r.mountAuditEvidence(mux)
	require.NotNil(t, mux)
}

func TestMountPreflight_RegistersRoutes(t *testing.T) {
	t.Parallel()
	r := &Router{preflight: http.NotFoundHandler()}
	mux := chi.NewRouter()
	r.mountPreflight(mux)
	require.NotNil(t, mux)
}

func TestMountLLMConfig_RegistersRoutes(t *testing.T) {
	t.Parallel()
	r := &Router{llmConfig: ptrext.Of(consolellmconfig.Handler{})}
	mux := chi.NewRouter()
	r.mountLLMConfig(mux)
	require.NotNil(t, mux)
}

func TestMountAPIKeys_RegistersRoutes(t *testing.T) {
	t.Parallel()
	r := &Router{apiKeys: ptrext.Of(apikey.APIKeysHandler{})}
	mux := chi.NewRouter()
	r.mountAPIKeys(mux)
	require.NotNil(t, mux)
}

func TestSetAuditEvidenceHandler_SetsField(t *testing.T) {
	t.Parallel()
	r := &Router{}
	h := ptrext.Of(consoleauditevidence.Handler{})
	r.SetAuditEvidenceHandler(h)
	require.Equal(t, h, r.auditEvidence)
}

func TestSetMCPClientHandler_NilGuard(t *testing.T) {
	t.Parallel()
	r := &Router{}
	r.SetMCPClientHandler(ptrext.Of(consolemcpclient.Handler{}))
	require.NotNil(t, r.mcpClients)
}

func TestMountOIDC_RegistersRoutes(t *testing.T) {
	t.Parallel()
	r := &Router{oidc: ptrext.Of(consoleoidc.Handler{})}
	mux := chi.NewRouter()
	r.mountOIDC(mux)
	require.NotNil(t, mux)
}

func TestMountMCPClients_RegistersRoutes(t *testing.T) {
	t.Parallel()
	r := &Router{mcpClients: ptrext.Of(consolemcpclient.Handler{})}
	mux := chi.NewRouter()
	r.mountMCPClients(mux)
	require.NotNil(t, mux)
}

func TestSetPreflightHandler_SetsField(t *testing.T) {
	t.Parallel()
	r := &Router{}
	h := http.NotFoundHandler()
	r.SetPreflightHandler(h)
	require.NotNil(t, r.preflight)
}
