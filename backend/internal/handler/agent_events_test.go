package handler

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/danew/cdk-recharge-system/internal/config"
	"github.com/danew/cdk-recharge-system/internal/db"
)

func openHandlerTestDB(t *testing.T) {
	t.Helper()
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "test.db"))
	t.Setenv("INSTALL_MODE", "wizard")
	if err := db.Init(&config.DatabaseConfig{}); err != nil {
		t.Fatalf("init: %v", err)
	}
	t.Cleanup(func() {
		if db.DB != nil {
			_ = db.DB.Close()
			db.DB = nil
		}
	})
}

func TestEnqueueAgentOrderEvents(t *testing.T) {
	openHandlerTestDB(t)

	agent, err := db.CreateAgentUser("hook-agent", "Str0ngPassw0rd2026", "钩子", nil)
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if err := db.UpdateAgentUserSettings(agent.ID, "https://example.com/hook", ""); err != nil {
		t.Fatalf("webhook url: %v", err)
	}

	order := db.AgentOrder{
		OrderNo:     "AGTESTDELIV1",
		AgentUserID: agent.ID,
		Plan:        "plus",
		PlanLabel:   "Plus",
		Count:       2,
		IssuedCount: 2,
		IssuedCodes: []string{"GPTD-AAAA", "GPTD-BBBB"},
		DeliveredAt: "2026-08-27 17:01:08",
		FailReason:  "",
	}
	enqueueAgentOrderDelivered(order)
	enqueueAgentOrderDelivered(order) // 幂等

	failOrder := order
	failOrder.OrderNo = "AGTESTFAIL1"
	failOrder.IssuedCount = 0
	failOrder.IssuedCodes = nil
	failOrder.FailReason = "卡台未返回完整卡密"
	enqueueAgentOrderFulfillFailed(failOrder)
	enqueueAgentOrderFulfillFailed(failOrder)

	list, total, err := db.ListAgentWebhookDeliveries(agent.ID, 1, 20)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 2 {
		t.Fatalf("want 2 events, got %d", total)
	}

	byType := map[string]db.AgentWebhookDelivery{}
	for _, d := range list {
		byType[d.EventType] = d
	}
	delivered, ok := byType[agentEventOrderDelivered]
	if !ok || delivered.EventID != "evt_AGTESTDELIV1_delivered" {
		t.Fatalf("missing delivered: %+v", list)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(mustPayload(t, delivered.EventID)), &body); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if body["event_type"] != agentEventOrderDelivered {
		t.Fatalf("event_type = %v", body["event_type"])
	}
	data, _ := body["data"].(map[string]any)
	if data["order_no"] != "AGTESTDELIV1" {
		t.Fatalf("order_no = %v", data["order_no"])
	}

	failed, ok := byType[agentEventOrderFulfillFailed]
	if !ok || failed.EventID != "evt_AGTESTFAIL1_fulfill_failed" {
		t.Fatalf("missing fulfill_failed: %+v", list)
	}
}

func mustPayload(t *testing.T, eventID string) string {
	t.Helper()
	var payload string
	if err := db.DB.QueryRow(`SELECT payload FROM agent_webhook_deliveries WHERE event_id = ?`, eventID).Scan(&payload); err != nil {
		t.Fatalf("payload %s: %v", eventID, err)
	}
	return payload
}

func TestEnqueueAgentOrderSkippedWithoutWebhook(t *testing.T) {
	openHandlerTestDB(t)
	agent, err := db.CreateAgentUser("no-hook", "Str0ngPassw0rd2026", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	enqueueAgentOrderDelivered(db.AgentOrder{
		OrderNo: "AGNOHOOK", AgentUserID: agent.ID, Plan: "plus", Count: 1, IssuedCodes: []string{"X"},
	})
	if _, total, err := db.ListAgentWebhookDeliveries(agent.ID, 1, 10); err != nil || total != 0 {
		t.Fatalf("未配回调不应入队: total=%d err=%v", total, err)
	}
}
