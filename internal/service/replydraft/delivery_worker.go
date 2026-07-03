// SPDX-License-Identifier: Apache-2.0

package replydraft

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/pkg/workerdrain"
	replydraftrepo "github.com/Phixsura/attune/internal/repo/replydraft"
)

// DeliveryWorker drains retry-due reply.send delivery attempts. Manual sends,
// hook tests, and manual redeliveries still execute inline; this worker closes
// the scheduled retry loop recorded by reply_delivery_attempts.next_retry_at.
type DeliveryWorker struct {
	workflow *Workflow
	owner    string
	drain    *workerdrain.Drainer

	pollInterval  time.Duration
	staleDuration time.Duration
	batchSize     int
}

func NewDeliveryWorker(workflow *Workflow) *DeliveryWorker {
	d := workerdrain.New("reply_delivery")
	d.SetTimeout(drainTimeout)
	return ptrext.Of(DeliveryWorker{
		workflow:      workflow,
		owner:         "reply_delivery-" + uuid.NewString(),
		drain:         d,
		pollInterval:  5 * time.Second,
		staleDuration: 10 * time.Minute,
		batchSize:     10,
	})
}

// Configure overrides defaults (0 = keep default).
func (w *DeliveryWorker) Configure(pollInterval time.Duration, batchSize int) {
	if pollInterval > 0 {
		w.pollInterval = pollInterval
	}
	if batchSize > 0 {
		w.batchSize = batchSize
	}
}

func (w *DeliveryWorker) Run(ctx context.Context) {
	const where = "service.replydraft.DeliveryWorker.Run"
	if reset, err := w.workflow.ResetStalePendingDeliveries(ctx, w.staleDuration); err != nil {
		logext.Warnf(ctx, "[%s] reset stale reply deliveries failed,err:%+v", where, err.Error())
	} else if reset > 0 {
		logext.Infof(ctx, "[%s] reset stale reply deliveries,count:%d", where, reset)
		metrics.WorkerStaleClaimsRecovered.WithLabelValues("reply_delivery").Add(float64(reset))
	}
	logext.Infof(ctx, "[%s] reply-delivery worker started,owner:%s,poll_interval:%s,batch:%d",
		where, w.owner, w.pollInterval, w.batchSize)

	tick := time.NewTicker(w.pollInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			logext.Infof(ctx, "[%s] reply-delivery worker stopping,waiting for in-flight work", where)
			w.drain.Drain(ctx)
			return
		case <-tick.C:
			w.ProcessOnce(ctx)
		}
	}
}

// ProcessOnce claims and sends one due retry batch. Exposed for tests.
func (w *DeliveryWorker) ProcessOnce(ctx context.Context) {
	const where = "service.replydraft.DeliveryWorker.ProcessOnce"
	preps, err := w.workflow.ClaimDueDeliveries(ctx, w.batchSize, w.actor())
	if err != nil {
		logext.Errorf(ctx, "[%s] claim due deliveries failed,err:%+v", where, err.Error())
		return
	}
	for _, prep := range preps {
		w.drain.Enter()
		func() {
			defer w.drain.Leave()
			w.processDelivery(ctx, prep)
		}()
	}
}

func (w *DeliveryWorker) processDelivery(ctx context.Context, prep replydraftrepo.DeliveryPrepare) {
	const where = "service.replydraft.DeliveryWorker.processDelivery"
	attempt, err := w.workflow.executeObservableDelivery(ctx, prep.Hook.TenantID, prep)
	if err != nil {
		logext.Warnf(ctx, "[%s] delivery failed,attempt_id:%s,event_type:%s,err:%+v",
			where, prep.AttemptID, prep.EventType, err.Error())
		return
	}
	logext.Infof(ctx, "[%s] delivery finished,attempt_id:%s,event_type:%s,status:%s",
		where, prep.AttemptID, prep.EventType, attempt.Status)
}

func (w *DeliveryWorker) actor() replydraftrepo.Actor {
	return replydraftrepo.Actor{Type: "system", ID: w.owner}
}
