package db

import "testing"

func testStrPtr(s string) *string { return &s }

// reserveViaBatch 走生产真实路径占位：CreateAdminRechargeBatch 会对带 cdk_code
// 的明细做条件 UPDATE。测试不能再调 db.ReserveAgentCDK —— 那个函数生产里没人用。
func reserveViaBatch(t *testing.T, code, batchID, itemStatus string) {
	t.Helper()
	batch := AdminRechargeBatch{
		BatchID: batchID, Operator: "agent:1", AgentUserID: 1,
		Source: "agent_api", Plan: "plus", Total: 1, Status: "running",
	}
	item := AdminRechargeItem{
		BatchID: batchID, Seq: 1, ClientRequestID: batchID + "-001",
		Plan: "plus", CredMode: "session", CDKCode: code, Status: itemStatus,
	}
	if err := CreateAdminRechargeBatch(batch, batchID, []AdminRechargeItem{item}); err != nil {
		t.Fatalf("create batch %s: %v", batchID, err)
	}
}

// 占位是真条件更新：同一张码第二次提交必须被拒，且整批回滚。
func TestCreateBatchReserveRejectsSecondBatch(t *testing.T) {
	openTestDB(t)
	code := "CDK-RESERVE-00000001"
	if err := SaveCardplatformCDKCode(9501, code, "CDK-RESERVE", "plus", 100); err != nil {
		t.Fatal(err)
	}
	reserveViaBatch(t, code, "rsv-1", "pending")
	if got := cdkStatusByCode(code); got != "reserved" {
		t.Fatalf("after first batch status=%q, want reserved", got)
	}

	batch := AdminRechargeBatch{
		BatchID: "rsv-2", Operator: "agent:1", AgentUserID: 1,
		Source: "agent_api", Plan: "plus", Total: 1, Status: "running",
	}
	item := AdminRechargeItem{
		BatchID: "rsv-2", Seq: 1, ClientRequestID: "rsv-2-001",
		Plan: "plus", CredMode: "session", CDKCode: code, Status: "pending",
	}
	err := CreateAdminRechargeBatch(batch, "rsv-2", []AdminRechargeItem{item})
	if err == nil {
		t.Fatal("second batch must be rejected while the code is reserved")
	}
	// 回滚：批次与明细都不该留下
	var n int
	if qerr := DB.QueryRow(`SELECT COUNT(*) FROM admin_recharge_batches WHERE batch_id = 'rsv-2'`).Scan(&n); qerr != nil {
		t.Fatal(qerr)
	}
	if n != 0 {
		t.Fatalf("rolled-back batch left %d row(s)", n)
	}
}

// 释放受「其它在途/未知明细」约束：只有本批已落终态才放回可重试。
func TestReleaseRespectsOtherInFlightItems(t *testing.T) {
	openTestDB(t)
	code := "CDK-RESERVE-00000002"
	if err := SaveCardplatformCDKCode(9502, code, "CDK-RESERVE", "plus", 100); err != nil {
		t.Fatal(err)
	}
	reserveViaBatch(t, code, "rel-1", "pending")

	// 明细还在途：不能放回
	if n, err := CountItemsBlockingCDKRelease(code); err != nil || n != 1 {
		t.Fatalf("blocking count=%d err=%v, want 1", n, err)
	}
	if err := ReleaseAgentCDK(code); err != nil {
		t.Fatalf("release: %v", err)
	}
	if got := cdkStatusByCode(code); got != "reserved" {
		t.Fatalf("must stay reserved while an item is in flight, got %q", got)
	}

	// 明细落终态后可以放回
	if err := PatchAdminRechargeItem("rel-1-001", AdminRechargeItemPatch{
		Status: testStrPtr("failed"), Message: testStrPtr("上游拒绝"),
	}); err != nil {
		t.Fatal(err)
	}
	if err := ReleaseAgentCDK(code); err != nil {
		t.Fatalf("release after terminal: %v", err)
	}
	if got := cdkStatusByCode(code); got != "unused" {
		t.Fatalf("after terminal status=%q, want unused", got)
	}
}

// 结果未知的明细必须继续锁住卡密：不能因为别处失败就自动放回。
func TestReleaseBlockedByUnknownItem(t *testing.T) {
	openTestDB(t)
	code := "CDK-RESERVE-00000003"
	if err := SaveCardplatformCDKCode(9503, code, "CDK-RESERVE", "plus", 100); err != nil {
		t.Fatal(err)
	}
	reserveViaBatch(t, code, "unk-1", "pending")
	if err := PatchAdminRechargeItem("unk-1-001", AdminRechargeItemPatch{
		Status: testStrPtr("unknown"), Message: testStrPtr("结果未知"),
	}); err != nil {
		t.Fatal(err)
	}
	if err := ReleaseAgentCDK(code); err != nil {
		t.Fatalf("release: %v", err)
	}
	if got := cdkStatusByCode(code); got != "reserved" {
		t.Fatalf("unknown item must keep the code reserved, got %q", got)
	}
}

func cdkStatusByCode(code string) string {
	var v string
	_ = DB.QueryRow(`SELECT COALESCE(status,'') FROM cardplatform_cdk_codes WHERE code = ?`, code).Scan(&v)
	return v
}
