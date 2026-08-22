package handler

import (
	"encoding/json"
	"log"
	"time"

	"github.com/danew/cdk-recharge-system/internal/db"
)

// 代理回调事件类型
const (
	agentEventRechargeCompleted = "recharge.completed"
	agentEventRechargeFailed    = "recharge.failed"
	agentEventRechargeUnknown   = "recharge.unknown"
	agentEventBatchCompleted    = "batch.completed"
)

// agentEventTypeForItemStatus 明细终态 → 事件类型。非终态返回空串，不产生事件。
func agentEventTypeForItemStatus(status string) string {
	switch status {
	case itemStatusSuccess:
		return agentEventRechargeCompleted
	case itemStatusFailed, itemStatusSkipped:
		return agentEventRechargeFailed
	case itemStatusUnknown:
		return agentEventRechargeUnknown
	default:
		return ""
	}
}

// enqueueAgentItemEvent 明细落终态时投递回调。
//
// 从批次执行器的 patchItem 统一进入，那里是所有状态流转的必经之路。
// 非终态、非代理批次、未配置回调地址三种情况都直接返回，不落库也不产生噪声。
func enqueueAgentItemEvent(requestID, status string) {
	eventType := agentEventTypeForItemStatus(status)
	if eventType == "" {
		return
	}
	agentID, rec, err := db.LookupAgentItemForEvent(requestID)
	if err != nil {
		log.Printf("[agent-webhook] lookup item %s failed: %v", requestID, err)
		return
	}
	if agentID <= 0 || rec == nil {
		return
	}
	target, _, err := db.GetAgentWebhookTarget(agentID)
	if err != nil || target == "" {
		return
	}
	eventID := "evt_" + requestID + "_" + status
	payload := agentEventPayload(eventID, eventType, map[string]any{
		"request_id":        rec.RequestID,
		"batch_id":          rec.BatchID,
		"seq":               rec.Seq,
		"client_reference":  rec.ClientReference,
		"plan":              rec.Plan,
		"status":            rec.Status,
		"message":           rec.Message,
		"account_email":     rec.AccountEmail,
		"upstream_order_id": rec.UpstreamOrderID,
	})
	if err := db.EnqueueAgentWebhook(agentID, eventID, eventType, rec.BatchID, rec.RequestID, payload, target); err != nil {
		log.Printf("[agent-webhook] enqueue %s failed: %v", eventID, err)
	}
}

// enqueueAgentBatchEvent 整批跑完后投递汇总回调，代理据此一次性对账。
func enqueueAgentBatchEvent(batchID string) {
	agentID, err := db.LookupBatchAgent(batchID)
	if err != nil || agentID <= 0 {
		return
	}
	target, _, err := db.GetAgentWebhookTarget(agentID)
	if err != nil || target == "" {
		return
	}
	batch, err := db.GetAgentRechargeBatch(agentID, batchID)
	if err != nil || batch == nil {
		return
	}
	eventID := "evt_" + batchID + "_batch_completed"
	payload := agentEventPayload(eventID, agentEventBatchCompleted, map[string]any{
		"batch_id": batch.BatchID,
		"plan":     batch.Plan,
		"status":   batch.Status,
		"message":  batch.Message,
		"total":    batch.Total,
		"success":  batch.Success,
		"failed":   batch.Failed,
		"skipped":  batch.Skipped,
		"unknown":  batch.Unknown,
	})
	if err := db.EnqueueAgentWebhook(agentID, eventID, agentEventBatchCompleted, batchID, "", payload, target); err != nil {
		log.Printf("[agent-webhook] enqueue %s failed: %v", eventID, err)
	}
}

func agentEventPayload(eventID, eventType string, data map[string]any) string {
	b, err := json.Marshal(map[string]any{
		"event_id":   eventID,
		"event_type": eventType,
		"created_at": time.Now().UTC().Format(time.RFC3339),
		"data":       data,
	})
	if err != nil {
		return "{}"
	}
	return string(b)
}
