package db

import (
	"fmt"
	"testing"
)

// mkAgentBatch 建一个属于 agentID 的批次，明细状态按 statuses 逐条给定。
func mkAgentBatch(t *testing.T, batchID string, agentID int64, statuses ...string) {
	t.Helper()
	batch := AdminRechargeBatch{
		BatchID: batchID, Operator: fmt.Sprintf("agent:%d", agentID), AgentUserID: agentID,
		Source: "agent_api", Plan: "plus", Total: len(statuses), Status: "running",
	}
	items := make([]AdminRechargeItem, 0, len(statuses))
	for i, st := range statuses {
		items = append(items, AdminRechargeItem{
			BatchID: batchID, Seq: i + 1,
			ClientRequestID: fmt.Sprintf("%s-%03d", batchID, i+1),
			ClientReference: fmt.Sprintf("ref-%d", i+1),
			Plan:            "plus", CredMode: "session", Status: st,
		})
	}
	if err := CreateAdminRechargeBatch(batch, batchID, items); err != nil {
		t.Fatalf("create batch %s: %v", batchID, err)
	}
}

// 在途额度按「明细条数」计，不能按批次数。
// 早期实现数的是 status='running' 的批次，一批 50 条也只记 1，闸门等于形同虚设。
func TestCountAgentInFlightCountsItemsNotBatches(t *testing.T) {
	openTestDB(t)

	// 一个批次 5 条：3 条在途（pending/processing/submitted），2 条已终态
	mkAgentBatch(t, "agb-1", 1, "pending", "processing", "submitted", "success", "failed")

	n, err := CountAgentInFlightRecharges(1)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 3 {
		t.Fatalf("in-flight = %d, want 3（按明细计数，不是按批次）", n)
	}

	// 第二个批次再加 2 条在途，总数应累加
	mkAgentBatch(t, "agb-2", 1, "issuing", "preparing")
	if n, _ = CountAgentInFlightRecharges(1); n != 5 {
		t.Fatalf("in-flight = %d, want 5", n)
	}

	// 别的代理的在途条目不能算进来
	mkAgentBatch(t, "agb-3", 2, "pending", "pending")
	if n, _ = CountAgentInFlightRecharges(1); n != 5 {
		t.Fatalf("agent 1 in-flight = %d, 混入了其他代理的条目", n)
	}
	if n, _ = CountAgentInFlightRecharges(2); n != 2 {
		t.Fatalf("agent 2 in-flight = %d, want 2", n)
	}
}

// 批次查询必须按 agent_user_id 隔离，代理拿不到别人的批次。
func TestAgentBatchQueriesIsolateAgents(t *testing.T) {
	openTestDB(t)

	mkAgentBatch(t, "agb-mine", 1, "success", "failed", "skipped", "unknown", "processing")
	mkAgentBatch(t, "agb-theirs", 2, "success")

	list, total, err := ListAgentRechargeBatches(1, 1, 20)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].BatchID != "agb-mine" {
		t.Fatalf("list leaked or missed batches: total=%d list=%+v", total, list)
	}
	got := list[0]
	if got.Success != 1 || got.Failed != 1 || got.Skipped != 1 || got.Unknown != 1 || got.Running != 1 {
		t.Fatalf("按状态聚合的计数不对: %+v", got)
	}

	if b, err := GetAgentRechargeBatch(1, "agb-theirs"); err != nil || b != nil {
		t.Fatalf("代理 1 不该取到代理 2 的批次: b=%+v err=%v", b, err)
	}
	items, err := ListAgentRechargeBatchItems(1, "agb-theirs")
	if err != nil {
		t.Fatalf("items: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("代理 1 不该取到代理 2 的明细: %+v", items)
	}
}

// 明细事件反查要能带出归属代理；管理端批次返回 0，据此跳过回调。
func TestLookupAgentItemForEvent(t *testing.T) {
	openTestDB(t)

	mkAgentBatch(t, "agb-evt", 7, "success")
	agentID, rec, err := LookupAgentItemForEvent("agb-evt-001")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if agentID != 7 || rec == nil || rec.RequestID != "agb-evt-001" || rec.ClientReference != "ref-1" {
		t.Fatalf("unexpected: agentID=%d rec=%+v", agentID, rec)
	}

	mkAgentBatch(t, "brc-admin", 0, "success")
	if agentID, _, err = LookupAgentItemForEvent("brc-admin-001"); err != nil || agentID != 0 {
		t.Fatalf("管理端批次应返回 agentID=0, got %d err=%v", agentID, err)
	}

	if agentID, rec, err = LookupAgentItemForEvent("does-not-exist"); err != nil || agentID != 0 || rec != nil {
		t.Fatalf("未知 request_id 应安静返回零值: %d %+v %v", agentID, rec, err)
	}
}

// 同一 event_id 重复入队只能留一行，否则进程重启后的状态对齐会造成重复投递。
func TestEnqueueAgentWebhookIsIdempotent(t *testing.T) {
	openTestDB(t)

	const eventID = "evt_agb-x-001_success"
	for i := 0; i < 3; i++ {
		if err := EnqueueAgentWebhook(5, eventID, "recharge.completed", "agb-x", "agb-x-001",
			`{"event_id":"`+eventID+`"}`, "https://example.com/hook"); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}
	list, total, err := ListAgentWebhookDeliveries(5, 1, 20)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("重复入队产生了 %d 行，应只有 1 行", total)
	}
	if list[0].Status != "pending" || list[0].Attempts != 0 {
		t.Fatalf("unexpected initial state: %+v", list[0])
	}

	// 别的代理看不到这条投递记录
	if _, total, _ = ListAgentWebhookDeliveries(6, 1, 20); total != 0 {
		t.Fatalf("投递日志跨代理泄露: total=%d", total)
	}
}

// 重试与失败的状态流转，以及只有 failed 才允许手动重投。
func TestAgentWebhookRetryLifecycle(t *testing.T) {
	openTestDB(t)

	if err := EnqueueAgentWebhook(9, "evt_a", "batch.completed", "agb-y", "", "{}", "https://example.com/hook"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	due, err := ListDueAgentWebhooks(10)
	if err != nil || len(due) != 1 {
		t.Fatalf("due = %d, err = %v, want 1 条到期", len(due), err)
	}
	id := due[0].ID

	// 推迟重试后不应再出现在到期列表里
	if err := MarkAgentWebhookRetry(id, 500, "boom", 3600); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if due, _ = ListDueAgentWebhooks(10); len(due) != 0 {
		t.Fatalf("退避期内仍被取出投递: %+v", due)
	}

	// failed 之前不允许重投
	if err := RequeueAgentWebhook(9, id); err == nil {
		t.Fatal("pending 状态不应允许手动重投")
	}
	if err := MarkAgentWebhookFailed(id, 500, "gave up"); err != nil {
		t.Fatalf("fail: %v", err)
	}
	// 不是自己的记录也不能重投
	if err := RequeueAgentWebhook(10, id); err == nil {
		t.Fatal("跨代理重投必须被拒绝")
	}
	if err := RequeueAgentWebhook(9, id); err != nil {
		t.Fatalf("requeue: %v", err)
	}
	if due, _ = ListDueAgentWebhooks(10); len(due) != 1 {
		t.Fatalf("重投后应立即到期, got %d", len(due))
	}

	if err := MarkAgentWebhookDelivered(id, 200); err != nil {
		t.Fatalf("delivered: %v", err)
	}
	list, _, _ := ListAgentWebhookDeliveries(9, 1, 20)
	if len(list) != 1 || list[0].Status != "delivered" || list[0].LastStatusCode != 200 {
		t.Fatalf("终态不对: %+v", list)
	}
}

// 老库里 max_concurrent_recharge 存的是按批次计数时代的 2，
// 迁移必须把它抬到新默认值，否则批量接口会被旧额度直接卡死。
func TestMigrationRaisesLegacyConcurrencyDefault(t *testing.T) {
	openTestDB(t)

	agent, err := CreateAgentUser("legacy", "Str0ngPassw0rd2026", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if agent.MaxConcurrentRecharge < AgentDefaultMaxConcurrent {
		t.Fatalf("max_concurrent_recharge = %d, want >= %d", agent.MaxConcurrentRecharge, AgentDefaultMaxConcurrent)
	}
	if agent.MaxBatchItems != AgentDefaultMaxBatchItems {
		t.Fatalf("max_batch_items = %d, want %d", agent.MaxBatchItems, AgentDefaultMaxBatchItems)
	}

	// 限额更新要落库并被硬上限夹住
	if err := UpdateAgentUserLimits(agent.ID, 120, 999, 999); err != nil {
		t.Fatalf("update limits: %v", err)
	}
	got, err := GetAgentUserByID(agent.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.RateLimitRPM != 120 {
		t.Fatalf("rpm = %d", got.RateLimitRPM)
	}
	if got.MaxConcurrentRecharge != AgentMaxConcurrentHardCap {
		t.Fatalf("并发未被硬上限夹住: %d", got.MaxConcurrentRecharge)
	}
	if got.MaxBatchItems != AgentMaxBatchItemsHardCap {
		t.Fatalf("单批条数未被硬上限夹住: %d", got.MaxBatchItems)
	}
}
