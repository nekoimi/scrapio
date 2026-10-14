package co

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/collector"
	"github.com/nekoimi/scrapio/internal/plugin"
	"strings"
	"sync"
	"time"
	"xorm.io/xorm"
)

type DeliveryBinding struct {
	ID         string                       `json:"id"`
	Port       string                       `json:"port"`
	PluginCode string                       `json:"plugin_code"`
	Version    string                       `json:"version"`
	Emit       string                       `json:"emit"`
	Mapping    map[string]collector.Binding `json:"mapping"`
}
type Delivery struct {
	ID              string `xorm:"id pk"`
	OwnerID         int64  `xorm:"owner_id"`
	RunID           string `xorm:"run_id"`
	OutputID        string `xorm:"output_id"`
	BindingID       string `xorm:"binding_id"`
	PluginCode      string `xorm:"plugin_code"`
	PluginVersion   string `xorm:"plugin_version"`
	BindingHash     string `xorm:"binding_hash"`
	IdempotencyKey  string `xorm:"idempotency_key"`
	Namespace       string
	Status          string
	Input           string `xorm:"jsonb input"`
	Error           string `xorm:"jsonb error"`
	Receipt         string `xorm:"jsonb receipt"`
	ExternalID      string `xorm:"external_id"`
	DestinationHash string `xorm:"destination_hash"`
	Attempts        int
	Revision        int
	LeaseToken      string `xorm:"lease_token"`
	LeaseUntil      *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (Delivery) TableName() string { return "co_deliveries" }

var deliveryConfig struct {
	sync.RWMutex
	registry *plugin.Registry
}

func ConfigureDeliveries(r *plugin.Registry) {
	deliveryConfig.Lock()
	deliveryConfig.registry = r
	deliveryConfig.Unlock()
}
func deliveryHandler(code string) (plugin.ReceiptHandler, bool) {
	deliveryConfig.RLock()
	r := deliveryConfig.registry
	deliveryConfig.RUnlock()
	if r == nil {
		return nil, false
	}
	h, ok := r.Get(code)
	if !ok {
		return nil, false
	}
	p, ok := h.(plugin.ReceiptHandler)
	return p, ok
}
func DeliveryCapabilities() any {
	_, configured := deliveryHandler("aria2")
	return map[string]any{"items": []any{map[string]any{"code": "aria2", "version": plugin.Aria2DeliveryVersion, "configured": configured, "fields": []string{"url", "dir"}, "receipt": "accepted_not_download_complete", "recovery": "query_original_gid"}}, "max_bindings": 5, "trial_external_actions": 0, "idempotency_scope": "output_binding", "cloud_driver_ready": false}
}
func validateDeliveries(st TaskStage, d collector.Definition) error {
	if len(st.Deliveries) > 5 {
		return collector.Invalid("deliveries", "每阶段最多5个交付绑定")
	}
	ids := map[string]bool{}
	for _, b := range st.Deliveries {
		loc := "stages." + st.ID + ".deliveries." + b.ID
		if !graphID.MatchString(b.ID) || ids[b.ID] {
			return collector.Invalid(loc, "交付ID无效或重复")
		}
		ids[b.ID] = true
		if b.PluginCode != "aria2" || b.Version != plugin.Aria2DeliveryVersion {
			return collector.Invalid(loc, "首版仅支持co.delivery.aria2.v1")
		}
		port, e := d.ForPort(b.Port)
		if e != nil {
			return collector.Invalid(loc+".port", "输出端口不存在")
		}
		save, e := st.Binding(b.Port)
		if e != nil {
			return e
		}
		if b.Emit != "valid" && b.Emit != "created" && b.Emit != "changed" {
			return collector.Invalid(loc+".emit", "传递策略需valid/created/changed")
		}
		if passThrough(save) && b.Emit != "valid" {
			return collector.Invalid(loc+".emit", "仅传递结果只能按valid交付")
		}
		if _, ok := b.Mapping["url"]; !ok || len(b.Mapping) > 2 {
			return collector.Invalid(loc+".mapping", "需要url及可选dir映射")
		}
		fields := map[string]collector.Field{}
		for _, f := range port.Output.Fields {
			fields[f.Key] = f
		}
		for key, m := range b.Mapping {
			if key != "url" && key != "dir" {
				return collector.Invalid(loc+".mapping."+key, "不支持该插件输入")
			}
			if m.Kind == "field" {
				f, ok := fields[strings.TrimPrefix(m.Path, "output.")]
				if !strings.HasPrefix(m.Path, "output.") || !ok || f.Multiple || f.Nullable || (f.Type != "string" && f.Type != "url") || !f.Required {
					return collector.Invalid(loc+".mapping."+key, "需映射已声明的必填非null字符串/URL单值输出")
				}
			} else if m.Kind == "constant" {
				v, e := collector.Resolve(m, nil)
				s, ok := v.(string)
				if e != nil || !ok || strings.TrimSpace(s) == "" || len(s) > 8192 {
					return collector.Invalid(loc+".mapping."+key, "常量需有限非空字符串")
				}
				if key == "url" {
					if e = plugin.ValidateAria2Delivery(map[string]any{"url": s}); e != nil {
						return collector.Invalid(loc+".mapping.url", e.Error())
					}
				}
				if key == "dir" && len(s) > 1024 {
					return collector.Invalid(loc+".mapping.dir", "dir最多1024字节")
				}
			} else {
				return collector.Invalid(loc+".mapping."+key, "交付只支持输出字段或显式常量")
			}
		}
	}
	return nil
}
func deliveryInput(b DeliveryBinding, output map[string]any) (map[string]any, error) {
	result := map[string]any{}
	for key, m := range b.Mapping {
		var v any
		var e error
		if m.Kind == "field" {
			var ok bool
			v, ok = output[strings.TrimPrefix(m.Path, "output.")]
			if !ok {
				return nil, collector.Invalid("deliveries."+b.ID+"."+key, "输出字段缺失")
			}
		} else {
			v, e = collector.Resolve(m, nil)
			if e != nil {
				return nil, e
			}
		}
		result[key] = v
	}
	if collector.Sensitive(result) {
		return nil, collector.Invalid("delivery", "插件输入不能携带裸凭据")
	}
	if e := plugin.ValidateAria2Delivery(result); e != nil {
		return nil, collector.Invalid("deliveries."+b.ID+".input", e.Error())
	}
	return result, nil
}
func emitOutput(emit, decision string) bool {
	return emit == "valid" || emit == "created" && decision == "created" || emit == "changed" && (decision == "created" || decision == "updated")
}
func registerDeliveries(s *xorm.Session, r *Run, c *WorkClaim, out SavedOutput) error {
	if len(c.Manifest.Stage(c.StageID).Deliveries) == 0 {
		return nil
	}
	var output map[string]any
	if e := collector.Strict([]byte(out.Values), &output); e != nil {
		return e
	}
	for _, b := range c.Manifest.Stage(c.StageID).Deliveries {
		if b.Port != out.Port || !emitOutput(b.Emit, out.Decision) {
			continue
		}
		input, e := deliveryInput(b, output)
		status := "queued"
		problem := "{}"
		if e != nil {
			status = "failed"
			problem = collector.JSON(collector.AsError(e))
		}
		if r.Namespace == "trial" && e == nil {
			status = "preview"
		}
		if e = reserveStorage(s, r.OwnerID, "output_bytes", int64(len(collector.JSON(input))+len(problem)+512+8192)); e != nil {
			return e
		}
		row := Delivery{ID: uuid.NewString(), OwnerID: r.OwnerID, RunID: r.ID, OutputID: out.ID, BindingID: b.ID, PluginCode: b.PluginCode, PluginVersion: b.Version, BindingHash: collector.Hash(b), IdempotencyKey: "co.delivery:" + collector.Hash([]string{out.ID, b.ID, collector.Hash(b)}), Namespace: r.Namespace, Status: status, Input: collector.JSON(input), Error: problem, Receipt: "{}", Revision: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}
		if _, e = s.Insert(&row); e != nil {
			return e
		}
	}
	return nil
}

func checkDeliveryBudget(s *xorm.Session, owner int64) error {
	// Backpressure is durable: do not silently drop deliveries when the worker is offline.
	n, e := s.Table("co_deliveries").Where("owner_id=? AND namespace='formal' AND status IN ('queued','sending','checking','awaiting_confirmation')", owner).Count()
	if e != nil {
		return e
	}
	if n > 10000 {
		return collector.Fault("DELIVERY_BUDGET", "delivery", "活动插件交付达到10000条，本页回滚；请处理积压")
	}
	return nil
}

type DeliveryControlRequest struct {
	DeliveryID       string `json:"delivery_id"`
	Action           string `json:"action"`
	ExpectedRevision int    `json:"expected_revision"`
	Confirmed        bool   `json:"confirmed"`
}

func ControlDelivery(ctx context.Context, owner int64, runID, key string, p DeliveryControlRequest) (*Operation, error) {
	if !p.Confirmed {
		return nil, collector.Invalid("confirmed", "请确认交付操作")
	}
	return Mutate(ctx, owner, "controlDelivery", runID, key, p, false, func(s *xorm.Session, o *Operation) (any, error) {
		r, e := getRun(s, owner, runID, true)
		if e != nil {
			return nil, e
		}
		d := new(Delivery)
		ok, e := s.SQL("SELECT * FROM co_deliveries WHERE id=? AND run_id=? AND owner_id=? FOR UPDATE", p.DeliveryID, runID, owner).Get(d)
		if e != nil {
			return nil, e
		}
		if !ok {
			return nil, ErrNotFound
		}
		if d.Revision != p.ExpectedRevision {
			return nil, conflict("交付状态已变化，请刷新后操作")
		}
		if d.Namespace != "formal" {
			return nil, conflict("试采只预演，不执行插件动作")
		}
		switch p.Action {
		case "query":
			if d.Status != "awaiting_confirmation" {
				return nil, conflict("仅未知回执可查询原请求")
			}
			d.Status = "checking"
		case "retry":
			if d.Status != "failed" || d.ExternalID != "" {
				return nil, conflict("只重试确认未提交的失败；未知或已提交请求不能重发")
			}
			if e = plugin.ValidateAria2Delivery(deliveryTask(*d).Input); e != nil {
				return nil, collector.Invalid("delivery.input", "冻结交付输入无效；请修正映射后发布新任务版本，不能重试原输入")
			}
			if r.Status == "cancelled" {
				return nil, conflict("已取消运行不能重发交付")
			}
			d.Status = "queued"
		case "confirm_delivered":
			if d.Status != "awaiting_confirmation" {
				return nil, conflict("仅未知回执需要人工确认")
			}
			d.Status = "succeeded"
			d.Receipt = collector.JSON(map[string]any{"manual": true, "operator": owner, "receipt": "accepted", "download_completion": "not_confirmed"})
		case "cancel":
			if d.Status != "queued" && d.Status != "failed" && d.Status != "awaiting_confirmation" {
				return nil, conflict("当前不能结束交付；正在执行时先等待原回执")
			}
			d.Status = "cancelled"
		default:
			return nil, collector.Invalid("action", "操作需query/retry/confirm_delivered/cancel")
		}
		_, e = s.Exec(`UPDATE co_deliveries SET status=?,revision=revision+1,error='{}',receipt=CAST(? AS jsonb),lease_token=NULL,lease_until=NULL,updated_at=NOW() WHERE id=?`, d.Status, d.Receipt, d.ID)
		if e != nil {
			return nil, e
		}
		if e = event(s, r, "delivery_"+p.Action, "", map[string]any{"delivery_id": d.ID, "status": d.Status}); e != nil {
			return nil, e
		}
		return map[string]any{"id": d.ID, "status": d.Status, "revision": d.Revision + 1}, nil
	})
}

func deliveryProblem(code, message string) *collector.Error {
	return &collector.Error{Code: code, Message: message, Location: map[string]string{"step_id": "delivery"}}
}
func deliveryTask(d Delivery) plugin.Task {
	input := map[string]any{}
	_ = json.Unmarshal([]byte(d.Input), &input)
	return plugin.Task{IdempotencyKey: d.IdempotencyKey, EventType: "co.output.persisted", Input: input}
}

func deliveryCounts(s *xorm.Session, runID string) ([]json.RawMessage, error) {
	return views(s, "SELECT status,COUNT(*) AS count FROM co_deliveries WHERE run_id=? GROUP BY status", runID)
}

func validateDeliveryProfile(d Delivery, h plugin.ReceiptHandler) error {
	if h.DeliveryVersion() != d.PluginVersion {
		return fmt.Errorf("plugin receipt contract changed")
	}
	return plugin.ValidateAria2Delivery(deliveryTask(d).Input)
}
