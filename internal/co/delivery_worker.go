package co

import (
	"context"
	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/collector"
	"github.com/nekoimi/scrapio/internal/plugin"
	log "github.com/sirupsen/logrus"
	"time"
	xormlog "xorm.io/xorm/log"
)

type DeliveryClaim struct {
	Delivery
	Recovery bool
	Handler  plugin.ReceiptHandler
}

// ClaimDelivery locks run -> delivery and journals the fixed external identity
// before sending. Expired sends can only query, never enter SubmitDelivery again.
func ClaimDelivery(ctx context.Context) (*DeliveryClaim, error) {
	s := session(ctx)
	defer s.Close()
	rows, e := s.QueryString(`SELECT d.id,d.run_id FROM co_deliveries d JOIN co_runs r ON r.id=d.run_id WHERE d.namespace='formal' AND ((d.status='queued' AND r.status NOT IN ('preparing','pausing','paused','cancelled')) OR (d.status IN ('sending','checking') AND (d.lease_until IS NULL OR d.lease_until<NOW()))) ORDER BY d.updated_at,d.id LIMIT 1`)
	if e != nil || len(rows) == 0 {
		return nil, e
	}
	if e = s.Begin(); e != nil {
		return nil, e
	}
	defer s.Rollback()
	r, e := getRun(s, 0, rows[0]["run_id"], true)
	if e != nil {
		return nil, e
	}
	d := new(Delivery)
	ok, e := s.SQL("SELECT * FROM co_deliveries WHERE id=? AND (status='queued' OR (status IN ('sending','checking') AND (lease_until IS NULL OR lease_until<NOW()))) FOR UPDATE", rows[0]["id"]).Get(d)
	if e != nil {
		return nil, e
	}
	if !ok {
		return nil, s.Commit()
	}
	recovery := d.Status != "queued"
	if !recovery && (r.Status == "preparing" || r.Status == "pausing" || r.Status == "paused" || r.Status == "cancelled") {
		return nil, s.Commit()
	}
	h, available := deliveryHandler(d.PluginCode)
	if !recovery && (!available || validateDeliveryProfile(*d, h) != nil) {
		_, e = s.Exec(`UPDATE co_deliveries SET status='failed',error=CAST(? AS jsonb),revision=revision+1,updated_at=NOW() WHERE id=?`, collector.JSON(deliveryProblem("DELIVERY_UNAVAILABLE", "插件未配置或契约/输入不兼容；尚未提交，可检查配置后重试")), d.ID)
		if e == nil {
			e = event(s, r, "delivery_blocked", "", map[string]any{"delivery_id": d.ID})
		}
		if e != nil {
			return nil, e
		}
		return nil, s.Commit()
	}
	if !recovery {
		d.ExternalID = h.DeliveryIdentity(deliveryTask(*d))
		d.DestinationHash = h.DeliveryDestination()
		if d.ExternalID == "" || d.DestinationHash == "" {
			return nil, collector.Invalid("delivery", "插件未提供固定回执身份与目标")
		}
	}
	d.Status = "sending"
	if recovery {
		d.Status = "checking"
	}
	token := uuid.NewString()
	// The database owns lease time; host/database clock skew cannot block recovery.
	_, e = s.Exec(`UPDATE co_deliveries SET status=?,external_id=?,destination_hash=?,lease_token=?,lease_until=NOW()+INTERVAL '90 seconds',attempts=attempts+1,revision=revision+1,updated_at=NOW() WHERE id=?`, d.Status, d.ExternalID, d.DestinationHash, token, d.ID)
	if e != nil {
		return nil, e
	}
	if e = event(s, r, "delivery_"+d.Status, "", map[string]any{"delivery_id": d.ID, "external_id": d.ExternalID}); e != nil {
		return nil, e
	}
	if _, e = s.SQL("SELECT * FROM co_deliveries WHERE id=?", d.ID).Get(d); e != nil {
		return nil, e
	}
	if e = s.Commit(); e != nil {
		return nil, e
	}
	return &DeliveryClaim{Delivery: *d, Recovery: recovery, Handler: h}, nil
}

func finishDelivery(ctx context.Context, c *DeliveryClaim, status string, receipt any, problem *collector.Error) error {
	raw := collector.JSON(receipt)
	if len(raw) > 8192 || collector.Sensitive(receipt) {
		raw = "{}"
		status = "awaiting_confirmation"
		problem = deliveryProblem("DELIVERY_RECEIPT_INVALID", "原回执超出预算或包含认证字段，外部请求未重发")
	}
	if receipt == nil {
		raw = "{}"
	}
	errJSON := "{}"
	if problem != nil {
		errJSON = collector.JSON(problem)
	}
	s := session(ctx)
	defer s.Close()
	if e := s.Begin(); e != nil {
		return e
	}
	defer s.Rollback()
	r, e := getRun(s, c.OwnerID, c.RunID, true)
	if e != nil {
		return e
	}
	result, e := s.Exec(`UPDATE co_deliveries SET status=?,receipt=CAST(? AS jsonb),error=CAST(? AS jsonb),lease_token=NULL,lease_until=NULL,revision=revision+1,updated_at=NOW() WHERE id=? AND lease_token=? AND lease_until>NOW()`, status, raw, errJSON, c.ID, c.LeaseToken)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return collector.Fault("LEASE_LOST", "delivery", "旧交付worker不能覆盖新回执")
	}
	if e = event(s, r, "delivery_"+status, "", map[string]any{"delivery_id": c.ID, "external_id": c.ExternalID}); e != nil {
		return e
	}
	return s.Commit()
}

// runDelivery can be exercised without a database/provider. A submit exception,
// timeout or lost response is reconciled using the original fixed identity only.
func runDelivery(ctx context.Context, c *DeliveryClaim) (status string, receipt any, problem *collector.Error) {
	status = "awaiting_confirmation"
	defer func() {
		if recover() != nil {
			status = "awaiting_confirmation"
			receipt = nil
			problem = deliveryProblem("DELIVERY_OUTCOME_UNKNOWN", "插件执行异常，查询原请求或人工确认；未重发")
		}
	}()
	h := c.Handler
	if h == nil || h.DeliveryVersion() != c.PluginVersion {
		return status, nil, deliveryProblem("DELIVERY_OUTCOME_UNKNOWN", "原回执插件不可用或契约已变化；未重发")
	}
	if c.DestinationHash == "" || h.DeliveryDestination() != c.DestinationHash {
		return status, nil, deliveryProblem("DELIVERY_DESTINATION_CHANGED", "原服务连接配置已变化；恢复原配置后查询，或人工核对原GID；未重发")
	}
	task := deliveryTask(c.Delivery)
	if !c.Recovery {
		output, external, e := h.SubmitDelivery(ctx, task)
		if e == nil && external == c.ExternalID {
			return "succeeded", output, nil
		}
	}
	output, outcome, e := h.QueryDelivery(ctx, task, c.ExternalID)
	if e == nil && outcome == "accepted" {
		return "succeeded", output, nil
	}
	if e == nil && outcome == "failed" {
		return "failed", output, deliveryProblem("DELIVERY_EXTERNAL_FAILED", "原请求已在外部服务失败或移除；不会重复提交")
	}
	return status, output, deliveryProblem("DELIVERY_OUTCOME_UNKNOWN", "原回执未确认，可能已经提交；请查询原请求或人工确认，未重发")
}

type DeliveryWorker struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func NewDeliveryWorker() *DeliveryWorker { return &DeliveryWorker{} }
func (*DeliveryWorker) Name() string     { return "coDeliveryWorker" }
func (w *DeliveryWorker) Start(parent context.Context) error {
	ctx, cancel := context.WithCancel(parent)
	ctx = context.WithValue(ctx, xormlog.SessionShowSQLKey, false)
	w.cancel = cancel
	w.done = make(chan struct{})
	go func() {
		defer close(w.done)
		timer := time.NewTicker(time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				c, e := ClaimDelivery(ctx)
				if e != nil {
					if ctx.Err() == nil {
						log.WithError(e).Warn("claim independent delivery")
					}
					continue
				}
				if c == nil {
					continue
				}
				attempt, stop := context.WithTimeout(ctx, 30*time.Second)
				status, receipt, problem := runDelivery(attempt, c)
				stop()
				commit, stopCommit := context.WithTimeout(context.Background(), 3*time.Second)
				e = finishDelivery(commit, c, status, receipt, problem)
				stopCommit()
				if e != nil {
					log.WithError(e).Warn("persist independent original delivery receipt")
				}
			}
		}
	}()
	return nil
}
func (w *DeliveryWorker) Stop(ctx context.Context) error {
	if w.cancel == nil {
		return nil
	}
	w.cancel()
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
