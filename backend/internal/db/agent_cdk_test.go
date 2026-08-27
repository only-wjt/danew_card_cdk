package db

import "testing"

func TestAgentCDKAssignAndRechargeGate(t *testing.T) {
	openTestDB(t)
	agent, err := CreateAgentUser("cdkagent", "AgentTestPass2026", "CDK Test", nil)
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	code := "CDK-TEST-PLUS-00000001"
	if err := SaveCardplatformCDKCode(9001, code, "CDK-TEST-PLUS", "plus", 100); err != nil {
		t.Fatalf("save cdk: %v", err)
	}
	if n, _, err := AssignCDKsToAgent(agent.ID, []string{code}); err != nil || n != 1 {
		t.Fatalf("assign: n=%d err=%v", n, err)
	}
	if err := CheckAgentCDKForRecharge(agent.ID, "plus", code); err != nil {
		t.Fatalf("check ok: %v", err)
	}
	if err := CheckAgentCDKForRecharge(agent.ID, "pro_5x", code); err != ErrCDKPlanMismatch {
		t.Fatalf("plan mismatch want ErrCDKPlanMismatch got %v", err)
	}
	other, err := CreateAgentUser("cdkagent2", "AgentTestPass2027", "Other", nil)
	if err != nil {
		t.Fatalf("create other: %v", err)
	}
	if err := CheckAgentCDKForRecharge(other.ID, "plus", code); err != ErrCDKWrongAgent {
		t.Fatalf("wrong agent want ErrCDKWrongAgent got %v", err)
	}
	unassigned := "CDK-TEST-PLUS-00000002"
	if err := SaveCardplatformCDKCode(9002, unassigned, "CDK-TEST-PLUS", "plus", 100); err != nil {
		t.Fatalf("save unassigned: %v", err)
	}
	if err := CheckAgentCDKForRecharge(agent.ID, "plus", unassigned); err != ErrCDKNotAssigned {
		t.Fatalf("unassigned want ErrCDKNotAssigned got %v", err)
	}
}

func TestCheckAgentCDKForRechargeRejectsLocalStock(t *testing.T) {
	openTestDB(t)
	agent, err := CreateAgentUser("whitecdka", "AgentTestPass2026", "W", nil)
	if err != nil {
		t.Fatalf("agent: %v", err)
	}
	if _, _, err := ImportLocalStockCodes(PlanGPTWhite, []string{"w@example.com"}); err != nil {
		t.Fatalf("import: %v", err)
	}
	if _, err := ClaimUnassignedLocalStock(agent.ID, PlanGPTWhite, 1); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := CheckAgentCDKForRecharge(agent.ID, PlanGPTWhite, "w@example.com"); err != ErrCDKLocalStock {
		t.Fatalf("want ErrCDKLocalStock got %v", err)
	}
}

func TestValidateAgentCDKBatchDedup(t *testing.T) {
	openTestDB(t)
	agent, err := CreateAgentUser("valagent", "AgentTestPass2026", "V", nil)
	if err != nil {
		t.Fatalf("agent: %v", err)
	}
	codes := []string{
		"CDK-DUP-A-00000001",
		"CDK-DUP-A-00000001",
		"CDK-DUP-B-00000002",
		"",
	}
	for i, c := range codes[:2] {
		if i == 1 {
			continue
		}
		_ = SaveCardplatformCDKCode(int64(9200+i), c, "CDK-DUP", "plus", 100)
	}
	_ = SaveCardplatformCDKCode(9202, codes[2], "CDK-DUP", "plus", 100)
	_, _, _ = AssignCDKsToAgent(agent.ID, []string{codes[0], codes[2]})

	sum := ValidateAgentCDKBatch(agent.ID, "plus", codes)
	if sum.TotalLines != 3 {
		t.Fatalf("total_lines=%d want 3", sum.TotalLines)
	}
	if sum.DuplicateLines != 1 {
		t.Fatalf("duplicate_lines=%d want 1", sum.DuplicateLines)
	}
	if sum.ValidCount != 2 {
		t.Fatalf("valid_count=%d want 2", sum.ValidCount)
	}
}

// 发错货要能收回，但已消耗的码不能动，否则会破坏对账。
func TestUnassignCDKsFromAgent(t *testing.T) {
	openTestDB(t)
	agent, err := CreateAgentUser("unassignagent", "AgentTestPass2026", "U", nil)
	if err != nil {
		t.Fatalf("agent: %v", err)
	}
	free := "CDK-UNASSIGN-00000001"
	used := "CDK-UNASSIGN-00000002"
	_ = SaveCardplatformCDKCode(9301, free, "CDK-UNASSIGN", "plus", 100)
	_ = SaveCardplatformCDKCode(9302, used, "CDK-UNASSIGN", "plus", 100)
	if n, _, err := AssignCDKsToAgent(agent.ID, []string{free, used}); err != nil || n != 2 {
		t.Fatalf("assign: n=%d err=%v", n, err)
	}
	if err := ConsumeAgentCDK(used); err != nil {
		t.Fatalf("consume: %v", err)
	}

	// codes 留空 = 收回全部未使用；已消耗的那张必须留在代理名下
	released, _, err := UnassignCDKsFromAgent(agent.ID, nil)
	if err != nil {
		t.Fatalf("unassign: %v", err)
	}
	if released != 1 {
		t.Fatalf("released=%d want 1", released)
	}
	if row, ok := LookupStoredCDKDetail(free); !ok || row.AssignedAgentUserID != 0 {
		t.Fatalf("free still assigned to %d", row.AssignedAgentUserID)
	}
	if row, ok := LookupStoredCDKDetail(used); !ok || row.AssignedAgentUserID != agent.ID {
		t.Fatalf("consumed cdk lost its owner")
	}

	stock, err := AgentCDKStockMap()
	if err != nil {
		t.Fatalf("stock: %v", err)
	}
	if s := stock[agent.ID]; s.Unused != 0 || s.Consumed != 1 {
		t.Fatalf("stock unused=%d consumed=%d want 0/1", s.Unused, s.Consumed)
	}
}

// 代充不落 session，客户凭卡密查账单要靠订单里的邮箱；成功那条优先。
func TestAccountEmailByCDK(t *testing.T) {
	openTestDB(t)
	code := "CDK-EMAIL-00000001"
	if err := SaveCardplatformCDKCode(9401, code, "CDK-EMAIL", "plus", 100); err != nil {
		t.Fatalf("save cdk: %v", err)
	}
	mk := func(batchID, email, status string) {
		t.Helper()
		batch := AdminRechargeBatch{
			BatchID: batchID, Operator: "agent:1", AgentUserID: 1,
			Source: "agent_api", Plan: "plus", Total: 1, Status: "done",
		}
		item := AdminRechargeItem{
			BatchID: batchID, Seq: 1, ClientRequestID: batchID + "-001",
			Plan: "plus", CredMode: "session", AccountEmail: email,
			CDKCode: code, Status: status,
		}
		if err := CreateAdminRechargeBatch(batch, batchID, []AdminRechargeItem{item}); err != nil {
			t.Fatalf("create batch %s: %v", batchID, err)
		}
	}
	mk("eml-1", "failed@example.com", "failed")
	mk("eml-2", "ok@example.com", "success")

	if got := AccountEmailByCDK(code); got != "ok@example.com" {
		t.Fatalf("email=%q want ok@example.com", got)
	}
	if got := AccountEmailByCDK("CDK-EMAIL-NOPE"); got != "" {
		t.Fatalf("unknown cdk should return empty, got %q", got)
	}
}

// 代理只能看到自己名下的卡密，且含完整码供复制。
func TestListAgentCDKInventoryIsolatesAgents(t *testing.T) {
	openTestDB(t)
	a1, err := CreateAgentUser("inv1", "AgentTestPass2026", "A1", nil)
	if err != nil {
		t.Fatalf("agent1: %v", err)
	}
	a2, err := CreateAgentUser("inv2", "AgentTestPass2027", "A2", nil)
	if err != nil {
		t.Fatalf("agent2: %v", err)
	}
	c1 := "CDK-INV-00000001"
	c2 := "CDK-INV-00000002"
	_ = SaveCardplatformCDKCode(9501, c1, "CDK-INV", "plus", 100)
	_ = SaveCardplatformCDKCode(9502, c2, "CDK-INV", "plus", 100)
	_, _, _ = AssignCDKsToAgent(a1.ID, []string{c1})
	_, _, _ = AssignCDKsToAgent(a2.ID, []string{c2})

	list, total, err := ListAgentCDKInventory(AgentCDKInventoryQuery{AgentUserID: a1.ID, Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].Code != c1 {
		t.Fatalf("agent1 inventory=%+v total=%d", list, total)
	}
	sum, err := AgentCDKInventorySummaryFor(a1.ID)
	if err != nil || sum.Unused != 1 || sum.Total != 1 {
		t.Fatalf("summary=%+v err=%v", sum, err)
	}
	list2, total2, err := ListAgentCDKInventory(AgentCDKInventoryQuery{AgentUserID: a2.ID, Status: "unused"})
	if err != nil || total2 != 1 || list2[0].Code != c2 {
		t.Fatalf("agent2 got %+v total=%d", list2, total2)
	}
}

func TestAgentCDKLifecycleOnTerminal(t *testing.T) {
	openTestDB(t)
	code := "CDK-TEST-LIFE-00000001"
	if err := SaveCardplatformCDKCode(9101, code, "CDK-TEST-LIFE", "plus", 100); err != nil {
		t.Fatalf("save: %v", err)
	}
	ReconcileCDKAfterItemTerminal(code, "success")
	_, _, _, st, ok := LookupStoredCDKByCode(code)
	if !ok || st != "consumed" {
		t.Fatalf("after success want consumed got %q ok=%v", st, ok)
	}
	code2 := "CDK-TEST-LIFE-00000002"
	_ = SaveCardplatformCDKCode(9102, code2, "CDK-TEST-LIFE", "plus", 100)
	_ = UpdateCardplatformCDKStatus(9102, "reserved")
	ReconcileCDKAfterItemTerminal(code2, "failed")
	_, _, _, st2, ok := LookupStoredCDKByCode(code2)
	if !ok || st2 != "unused" {
		t.Fatalf("after failed want unused got %q", st2)
	}
}
